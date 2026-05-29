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
	"time"

	"github.com/sirupsen/logrus"
	"github.com/swaggest/swgui/v5emb"

	"4dl/internal/logging"
)

const (
	defaultTorrentListenAddr = ":0"
	openAPISpecContentType   = "application/vnd.oai.openapi+json; charset=utf-8"
	slowRequestThreshold     = 2 * time.Second
)

func Run(ctx context.Context, cfg Config) error {
	startedAt := time.Now()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.InputPath == "" {
		cfg.InputPath = "-"
	}
	if cfg.TorrentListenAddr == "" {
		cfg.TorrentListenAddr = defaultTorrentListenAddr
	}

	items, err := loadSearchResults(cfg.InputPath, cfg.CryptoKey)
	if err != nil {
		return err
	}
	manager, err := NewManager(items, cfg.SaveTo, cfg.TorrentListenAddr)
	if err != nil {
		return err
	}
	defer manager.Close()

	staticFS, err := fs.Sub(StaticFiles, "static")
	if err != nil {
		return fmt.Errorf("init static fs: %w", err)
	}
	server := &Server{
		manager:  manager,
		staticFS: staticFS,
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %q: %w", cfg.ListenAddr, err)
	}
	defer listener.Close()

	logrus.WithFields(logrus.Fields{
		"listen":         listener.Addr().String(),
		"web_url":        "http://" + listener.Addr().String(),
		"items":          len(items),
		"input":          inputLabel(cfg.InputPath),
		"save_to":        cfg.SaveTo,
		"torrent_listen": cfg.TorrentListenAddr,
		"duration_ms":    logging.DurationMillis(time.Since(startedAt)),
	}).Info("get server listening")
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
		logrus.Info("get server stopped")
		return nil
	}
	logrus.WithError(err).Error("get server stopped unexpectedly")
	return err
}

type Server struct {
	manager  *Manager
	staticFS fs.FS
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPI)
	mux.Handle("GET /api/docs/", v5emb.New("404 Downloader Get API", "/api/openapi.json", "/api/docs/"))
	mux.HandleFunc("GET /api/docs", redirectToDocs)
	mux.HandleFunc("GET /api/torrents", s.handleListTorrents)
	mux.HandleFunc("GET /api/torrents/stream", s.handleStreamTorrents)
	mux.HandleFunc("GET /api/torrents/{id}", s.handleGetTorrent)
	mux.HandleFunc("POST /api/torrents/{id}/start", s.handleStartDownload)
	mux.HandleFunc("POST /api/torrents/{id}/pause", s.handlePauseDownload)
	mux.HandleFunc("POST /api/torrents/{id}/delete", s.handleDeleteDownload)
	mux.HandleFunc("GET /", s.handleStatic)
	return requestLogger(mux)
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", openAPISpecContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpec)
}

func redirectToDocs(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/api/docs/", http.StatusMovedPermanently)
}

func (s *Server) handleListTorrents(w http.ResponseWriter, r *http.Request) {
	state := s.manager.State(r.Context())
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"count": len(state.Torrents),
	})).Debug("get torrent list returned")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleStreamTorrents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	setSSEHeaders(w)

	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Debug("get torrent list stream opened")
	defer logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Debug("get torrent list stream closed")

	if !s.writeTorrentListEvent(w, rc, r) {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !s.writeTorrentListEvent(w, rc, r) {
				return
			}
		}
	}
}

func (s *Server) handleGetTorrent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, ok := s.manager.TorrentState(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Debug("get torrent returned")
	writeJSON(w, http.StatusOK, state)
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func (s *Server) writeTorrentListEvent(w http.ResponseWriter, rc *http.ResponseController, r *http.Request) bool {
	state := s.manager.State(r.Context())
	if err := writeSSE(w, "state", state); err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Warn("get torrent list stream write failed")
		return false
	}
	if err := rc.Flush(); err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Warn("get torrent list stream flush failed")
		return false
	}
	return true
}

func (s *Server) handleStartDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok, err := s.manager.StartDownload(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get torrent download started")
	writeTorrentState(w, r, s.manager, id, http.StatusAccepted)
}

func (s *Server) handlePauseDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok, err := s.manager.PauseDownload(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get torrent download paused")
	writeTorrentState(w, r, s.manager, id, http.StatusOK)
}

func (s *Server) handleDeleteDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok, err := s.manager.DeleteDownload(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get torrent download deleted")
	writeTorrentState(w, r, s.manager, id, http.StatusOK)
}

func writeTorrentState(w http.ResponseWriter, r *http.Request, manager *Manager, torrentID string, status int) {
	state, ok := manager.TorrentState(torrentID)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
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
