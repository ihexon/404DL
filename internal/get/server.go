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

	"mvdl/internal/logging"
)

const (
	defaultTorrentListenAddr = ":42069"
	slowRequestThreshold     = 2 * time.Second
)

func Run(ctx context.Context, cfg Config) error {
	startedAt := time.Now()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:6570"
	}
	if cfg.InputPath == "" {
		cfg.InputPath = "-"
	}
	if cfg.TorrentListenAddr == "" {
		cfg.TorrentListenAddr = defaultTorrentListenAddr
	}

	items, err := loadQueryResults(cfg.InputPath, cfg.CryptoKey)
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
		manager:    manager,
		staticFS:   staticFS,
		fileServer: http.FileServer(http.FS(staticFS)),
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
	manager    *Manager
	staticFS   fs.FS
	fileServer http.Handler
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleGetState)
	mux.HandleFunc("GET /api/state/stream", s.handleStreamState)
	mux.HandleFunc("POST /api/torrents/{id}/files/download", s.handleDownloadFile)
	mux.HandleFunc("POST /api/downloads/{id}/pause", s.handlePauseDownload)
	mux.HandleFunc("POST /api/downloads/{id}/resume", s.handleResumeDownload)
	mux.HandleFunc("POST /api/downloads/{id}/cancel", s.handleCancelDownload)
	mux.HandleFunc("POST /api/downloads/{id}/delete", s.handleDeleteDownload)
	mux.HandleFunc("GET /", s.handleStatic)
	return requestLogger(mux)
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	state := s.manager.State(r.Context())
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"count": len(state.Torrents),
	})).Debug("get state returned")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleStreamState(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Error("get state stream failed: response writer cannot flush")
		writeJSON(w, http.StatusInternalServerError, APIError{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Debug("get state stream opened")
	defer logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Debug("get state stream closed")

	if !s.writeStateEvent(w, flusher, r) {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !s.writeStateEvent(w, flusher, r) {
				return
			}
		}
	}
}

func (s *Server) writeStateEvent(w http.ResponseWriter, flusher http.Flusher, r *http.Request) bool {
	state := s.manager.State(r.Context())
	if err := writeSSE(w, "state", state); err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{})).Warn("get state stream write failed")
		return false
	}
	flusher.Flush()
	return true
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req FileDownloadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIError{Error: err.Error()})
		return
	}
	_, ok, err := s.manager.DownloadFile(r.Context(), id, req.Path)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id":   id,
		"file": req.Path,
	})).Info("get file download scheduled")
	writeJSON(w, http.StatusAccepted, s.manager.State(r.Context()))
}

func (s *Server) handlePauseDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.manager.PauseDownload(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "download not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get download paused")
	writeJSON(w, http.StatusOK, s.manager.State(r.Context()))
}

func (s *Server) handleResumeDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.manager.ResumeDownload(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "download not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get download resumed")
	writeJSON(w, http.StatusOK, s.manager.State(r.Context()))
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.manager.CancelDownload(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "download not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Info("get download canceled")
	writeJSON(w, http.StatusOK, s.manager.State(r.Context()))
}

func (s *Server) handleDeleteDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req DeleteDownloadRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIError{Error: err.Error()})
		return
	}
	ok, err := s.manager.DeleteDownload(r.Context(), id, req.DeleteFiles)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "download not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id":           id,
		"delete_files": req.DeleteFiles,
	})).Info("get download deleted")
	writeJSON(w, http.StatusOK, s.manager.State(r.Context()))
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, APIError{Error: "api endpoint not found"})
		return
	}
	staticPath := strings.TrimPrefix(r.URL.Path, "/")
	if staticPath == "" {
		staticPath = "index.html"
	}
	if _, err := fs.Stat(s.staticFS, staticPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.URL.Path = "/index.html"
		staticPath = "index.html"
	}
	if strings.HasPrefix(staticPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	s.fileServer.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func decodeJSON(r *http.Request, value any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func decodeOptionalJSON(r *http.Request, value any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return decodeJSON(r, value)
}

func writeSSE(w http.ResponseWriter, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
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
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
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
