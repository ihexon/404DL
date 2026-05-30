package get

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/swaggest/swgui/v5emb"

	"4dl/internal/logging"
	"4dl/internal/model"
	"4dl/internal/provider"
)

const (
	defaultTorrentListenAddr = ":0"
	defaultSearchLimit       = 50
	openAPISpecContentType   = "application/vnd.oai.openapi+json; charset=utf-8"
	slowRequestThreshold     = 2 * time.Second
)

func Run(ctx context.Context, cfg Config) error {
	startedAt := time.Now()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.TorrentListenAddr == "" {
		cfg.TorrentListenAddr = defaultTorrentListenAddr
	}
	if cfg.DefaultLimit <= 0 {
		cfg.DefaultLimit = defaultSearchLimit
	}

	downloadDir, err := resolveDownloadDir(cfg.DownloadDir)
	if err != nil {
		return err
	}
	stateDir, err := resolveStateDir(cfg.StateDir)
	if err != nil {
		return err
	}
	store, err := openTaskStore(stateDir)
	if err != nil {
		return err
	}
	records, err := store.Load()
	if err != nil {
		_ = store.Close()
		return err
	}
	manager, err := NewManager(records, downloadDir, stateDir, cfg.TorrentListenAddr, store)
	if err != nil {
		_ = store.Close()
		return err
	}
	defer manager.Close()

	staticFS, err := fs.Sub(StaticFiles, "static")
	if err != nil {
		return fmt.Errorf("init static fs: %w", err)
	}
	server := &Server{
		manager:      manager,
		staticFS:     staticFS,
		searcher:     cfg.Searcher,
		defaultLimit: cfg.DefaultLimit,
		stateChanged: make(chan struct{}),
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %q: %w", cfg.ListenAddr, err)
	}
	defer listener.Close()

	logrus.WithFields(logrus.Fields{
		"listen":         listener.Addr().String(),
		"web_url":        "http://" + listener.Addr().String(),
		"items":          len(records),
		"download_dir":   downloadDir,
		"state_dir":      stateDir,
		"torrent_listen": cfg.TorrentListenAddr,
		"duration_ms":    logging.DurationMillis(time.Since(startedAt)),
	}).Info("web server listening")
	httpServer := &http.Server{Handler: server.routes()}
	go func() {
		<-ctx.Done()
		logrus.WithField("timeout", "5s").Info("get shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Error("get shutdown failed")
		}
	}()
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		logrus.Info("web server stopped")
		return nil
	}
	logrus.WithError(err).Error("web server stopped unexpectedly")
	return err
}

type Server struct {
	manager       *Manager
	staticFS      fs.FS
	searcher      Searcher
	defaultLimit  int
	searchMu      sync.Mutex
	searchResults []model.SearchResult
	stateMu       sync.Mutex
	stateChanged  chan struct{}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealth)
	mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPI)
	mux.Handle("GET /api/docs/", v5emb.New("404 Downloader API", "/api/openapi.json", "/api/docs/"))
	mux.HandleFunc("GET /api/docs", redirectToDocs)
	mux.HandleFunc("POST /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/tasks/stream", s.handleStreamTasks)
	mux.HandleFunc("GET /api/tasks/stream2", s.handleTaskStreamSnapshot)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("PUT /api/tasks/{id}/continue", s.handleStartTask)
	mux.HandleFunc("PUT /api/tasks/{id}/pause", s.handlePauseTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.handleDeleteTask)
	mux.HandleFunc("GET /", s.handleStatic)
	return requestLogger(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", openAPISpecContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpec)
}

func redirectToDocs(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/api/docs/", http.StatusMovedPermanently)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if s.searcher == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIError{Error: "search is not configured"})
		return
	}
	req, ok := s.parseSearchRequestBody(w, r)
	if !ok {
		return
	}
	s.setSearchResults(nil)
	s.notifyStateChanged()
	results, err := s.searcher.Search(r.Context(), provider.SearchRequest{
		Query:     req.Query,
		Limit:     req.Limit,
		Providers: req.Providers,
	})
	if err != nil {
		if errors.Is(err, provider.ErrUnknownProvider) {
			writeJSON(w, http.StatusBadRequest, APIError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, APIError{Error: err.Error()})
		return
	}
	s.setSearchResults(results)
	s.notifyStateChanged()
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) parseSearchRequestBody(w http.ResponseWriter, r *http.Request) (SearchRequest, bool) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIError{Error: "invalid JSON request body"})
		return SearchRequest{}, false
	}
	req.Query = strings.TrimSpace(req.Query)
	req.Providers = normalizedValues(req.Providers)
	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, APIError{Error: "query is required"})
		return SearchRequest{}, false
	}
	if req.Limit < 0 {
		writeJSON(w, http.StatusBadRequest, APIError{Error: "limit must be a positive integer"})
		return SearchRequest{}, false
	}
	if req.Limit == 0 {
		req.Limit = s.defaultLimit
	}
	return req, true
}

func normalizedValues(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	state := s.appState(r.Context())
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"count": len(state.Tasks),
	})).Debug("get task list returned")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIError{Error: "invalid JSON request body"})
		return
	}
	var state TaskState
	var err error
	if strings.TrimSpace(req.MagnetURL) != "" {
		state, err = s.manager.CreateMagnetTask(req.Title, req.MagnetURL, req.Path)
	} else {
		state, err = s.manager.CreateTask(req.Result, req.Path)
	}
	if err != nil {
		if errors.Is(err, errSearchResultMissingHash) {
			writeJSON(w, http.StatusBadRequest, APIError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	s.notifyStateChanged()
	writeJSON(w, http.StatusCreated, state)
}

func (s *Server) handleStreamTasks(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	setSSEHeaders(w)

	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Debug("get task list stream opened")
	defer logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Debug("get task list stream closed")

	if !s.writeTaskListEvent(w, rc, r) {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		stateChanged := s.stateChangeChan()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-stateChanged:
		}
		if !s.writeTaskListEvent(w, rc, r) {
			return
		}
	}
}

func (s *Server) handleTaskStreamSnapshot(w http.ResponseWriter, r *http.Request) {
	state := s.appState(r.Context())
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"count": len(state.Tasks),
	})).Debug("get task stream snapshot returned")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, ok := s.manager.TaskState(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "task not found"})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Debug("get task returned")
	writeJSON(w, http.StatusOK, state)
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func (s *Server) writeTaskListEvent(w http.ResponseWriter, rc *http.ResponseController, r *http.Request) bool {
	state := s.appState(r.Context())
	if err := writeSSE(w, "state", state); err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Warn("get task list stream write failed")
		return false
	}
	if err := rc.Flush(); err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Warn("get task list stream flush failed")
		return false
	}
	return true
}

func (s *Server) appState(ctx context.Context) AppState {
	state := s.manager.State(ctx)
	state.SearchResults = s.currentSearchResults()
	return state
}

func (s *Server) setSearchResults(results []model.SearchResult) {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	s.searchResults = append([]model.SearchResult(nil), results...)
}

func (s *Server) currentSearchResults() []model.SearchResult {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	if s.searchResults == nil {
		return []model.SearchResult{}
	}
	return append([]model.SearchResult(nil), s.searchResults...)
}

func (s *Server) handleStartTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok, err := s.manager.StartTask(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "task not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get task started")
	s.notifyStateChanged()
	writeTaskState(w, r, s.manager, id, http.StatusAccepted)
}

func (s *Server) handlePauseTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok, err := s.manager.PauseTask(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "task not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get task paused")
	s.notifyStateChanged()
	writeTaskState(w, r, s.manager, id, http.StatusOK)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force := r.FormValue("force") == "true"
	state, ok, err := s.manager.DeleteTask(r.Context(), id, force)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "task not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id":    id,
		"force": force,
	})).Info("get task deleted")
	s.notifyStateChanged()
	writeJSON(w, http.StatusOK, TaskState{
		TaskItem: state,
		Runtime:  RuntimeView{Status: RuntimeStatusInactive},
	})
}

func (s *Server) stateChangeChan() <-chan struct{} {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.stateChanged == nil {
		s.stateChanged = make(chan struct{})
	}
	return s.stateChanged
}

func (s *Server) notifyStateChanged() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.stateChanged == nil {
		s.stateChanged = make(chan struct{})
	}
	close(s.stateChanged)
	s.stateChanged = make(chan struct{})
}

func writeTaskState(w http.ResponseWriter, r *http.Request, manager *Manager, taskID string, status int) {
	state, ok := manager.TaskState(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "task not found"})
		return
	}
	writeJSON(w, status, state)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, APIError{Error: "api endpoint not found"})
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFileFS(w, r, s.staticFS, name)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := fs.Stat(s.staticFS, name); err == nil {
		serveStaticName(w, r, s.staticFS, name)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	serveStaticName(w, r, s.staticFS, "index.html")
}

func serveStaticName(w http.ResponseWriter, r *http.Request, filesystem fs.FS, name string) {
	if name != "index.html" {
		http.ServeFileFS(w, r, filesystem, name)
		return
	}
	indexRequest := r.Clone(r.Context())
	indexRequest.URL.Path = "/"
	http.ServeFileFS(w, indexRequest, filesystem, "index.html")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeSSE(w http.ResponseWriter, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *responseRecorder) Flush() {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	rc := http.NewResponseController(r.ResponseWriter)
	_ = rc.Flush()
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		requestID := logging.RequestIDFromHTTP(r)
		w.Header().Set(logging.RequestIDHeader, requestID)

		recorder := &responseRecorder{ResponseWriter: w}
		ctx := logging.WithRequestID(r.Context(), requestID)
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			duration := time.Since(startedAt)
			fields := logging.HTTPRequestFields(r, requestID)
			fields["status"] = http.StatusInternalServerError
			fields["bytes"] = recorder.bytes
			fields["duration_ms"] = logging.DurationMillis(duration)
			fields["panic"] = fmt.Sprint(recovered)
			fields["stack"] = string(debug.Stack())
			logrus.WithFields(fields).Error("get request panicked")
			if recorder.status == 0 {
				http.Error(recorder, "internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(recorder, r.WithContext(ctx))

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(startedAt)
		fields := logging.HTTPRequestFields(r, requestID)
		fields["status"] = status
		fields["bytes"] = recorder.bytes
		fields["duration_ms"] = logging.DurationMillis(duration)
		entry := logrus.WithFields(fields)
		switch {
		case status >= 500:
			entry.Error("get request completed")
		case status >= 400:
			entry.Warn("get request completed")
		case duration >= slowRequestThreshold:
			entry.Info("get request completed")
		default:
			entry.Debug("get request completed")
		}
	})
}
