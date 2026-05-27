package httpfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/sirupsen/logrus"

	"mvdl/internal/logging"
)

const (
	defaultTorrentListenAddr = ":42069"
	minDownloadReadahead     = 16 * 1024 * 1024
	maxDownloadReadahead     = 256 * 1024 * 1024
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
	manager, err := NewManager(items, cfg.DataDir, cfg.ListenAddr, cfg.TorrentListenAddr)
	if err != nil {
		return err
	}
	defer manager.Close()

	server := &Server{manager: manager}
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
		"data_dir":       cfg.DataDir,
		"torrent_listen": cfg.TorrentListenAddr,
		"duration_ms":    logging.DurationMillis(time.Since(startedAt)),
	}).Info("httpfs server listening")
	httpServer := &http.Server{Handler: server.routes()}
	go func() {
		<-ctx.Done()
		logrus.WithField("timeout", "5s").Info("httpfs shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Error("httpfs shutdown failed")
		}
	}()
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		logrus.Info("httpfs server stopped")
		return nil
	}
	logrus.WithError(err).Error("httpfs server stopped unexpectedly")
	return err
}

type Server struct {
	manager *Manager
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/torrents", s.handleListTorrents)
	mux.HandleFunc("GET /api/torrents/{id}", s.handleGetTorrent)
	mux.HandleFunc("GET /api/torrents/{id}/files", s.handleGetFiles)
	mux.HandleFunc("GET /api/torrents/{id}/runtime", s.handleGetRuntime)
	mux.HandleFunc("GET /api/torrents/{id}/runtime/stream", s.handleStreamRuntime)
	mux.HandleFunc("GET /d/{id}/{filePath...}", s.handleDownload)
	mux.HandleFunc("GET /", s.handleStatic)
	return requestLogger(mux)
}

func (s *Server) handleListTorrents(w http.ResponseWriter, r *http.Request) {
	items := s.manager.List()
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"count": len(items),
	})).Debug("httpfs torrent list returned")
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetTorrent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, ok := s.manager.Get(id)
	if !ok {
		logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"id": id,
		})).Warn("httpfs torrent get failed: torrent not found")
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id":     id,
		"status": item.Status,
		"files":  len(item.Files),
	})).Debug("httpfs torrent returned")
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleGetFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, ok := s.manager.EnsureFiles(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id":     id,
		"status": item.Status,
		"files":  len(item.Files),
	})).Debug("httpfs torrent files returned")
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleGetRuntime(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snapshot, ok, err := s.manager.RuntimeSnapshot(id)
	if !ok {
		logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"id": id,
		})).Warn("httpfs runtime get failed: torrent not found")
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	if err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"id": id,
		})).Warn("httpfs runtime get failed")
		writeJSON(w, http.StatusConflict, APIError{Error: err.Error()})
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id":     id,
		"peers":  len(snapshot.Peers),
		"events": len(snapshot.Events),
	})).Debug("httpfs runtime returned")
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleStreamRuntime(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"id": id,
		})).Error("httpfs runtime stream failed: response writer cannot flush")
		writeJSON(w, http.StatusInternalServerError, APIError{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Debug("httpfs runtime stream opened")
	defer logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"id": id,
	})).Debug("httpfs runtime stream closed")

	if !s.writeRuntimeEvent(w, flusher, r, id) {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !s.writeRuntimeEvent(w, flusher, r, id) {
				return
			}
		}
	}
}

func (s *Server) writeRuntimeEvent(w http.ResponseWriter, flusher http.Flusher, r *http.Request, id string) bool {
	snapshot, ok, err := s.manager.RuntimeSnapshot(id)
	if !ok {
		writeSSEError(w, "torrent not found")
		flusher.Flush()
		return false
	}
	if err != nil {
		writeSSEError(w, err.Error())
		flusher.Flush()
		return false
	}
	if err := writeSSE(w, "runtime", snapshot); err != nil {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"id": id,
		})).Warn("httpfs runtime stream write failed")
		return false
	}
	flusher.Flush()
	return true
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	id := r.PathValue("id")
	filePath := r.PathValue("filePath")
	fields := logging.MergeFields(r.Context(), logrus.Fields{
		"id":   id,
		"file": filePath,
	})
	if filePath == "" {
		logrus.WithFields(fields).Warn("httpfs download rejected: empty file path")
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	t, ok, err := s.manager.FileTorrent(r.Context(), id)
	if !ok {
		logrus.WithFields(fields).Warn("httpfs download failed: torrent not found")
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Warn("httpfs download failed: metadata unavailable")
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}

	var selected *torrent.File
	for _, file := range t.Files() {
		if file.DisplayPath() == filePath {
			selected = file
			break
		}
	}
	if selected == nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithFields(fields).Warn("httpfs download failed: file not found")
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	reader := selected.NewReader()
	reader.SetContext(r.Context())
	reader.SetResponsive()
	reader.SetReadaheadFunc(downloadReadahead)
	defer reader.Close()

	fields["bytes"] = selected.Length()
	fields["info_hash"] = t.InfoHash().HexString()
	fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
	logrus.WithFields(fields).Info("httpfs download stream started")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", downloadETag(t.InfoHash().HexString(), filePath, selected.Length()))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": path.Base(filePath),
	}))
	http.ServeContent(w, r, path.Base(filePath), time.Time{}, reader)
}

func downloadETag(infoHash, filePath string, size int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", infoHash, filePath, size)))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func downloadReadahead(ctx torrent.ReadaheadContext) int64 {
	contiguous := ctx.CurrentPos - ctx.ContiguousReadStartPos
	if contiguous < minDownloadReadahead {
		return minDownloadReadahead
	}
	if contiguous > maxDownloadReadahead {
		return maxDownloadReadahead
	}
	return contiguous
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(StaticFiles, "static")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fileServer := http.FileServer(http.FS(staticFS))
	staticPath := strings.TrimPrefix(r.URL.Path, "/")
	if staticPath == "" {
		staticPath = "index.html"
	}
	if _, err := fs.Stat(staticFS, staticPath); err == nil {
		logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"asset": staticPath,
		})).Debug("httpfs static asset served")
		fileServer.ServeHTTP(w, r)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		logrus.WithError(err).WithFields(logging.MergeFields(r.Context(), logrus.Fields{
			"asset": staticPath,
		})).Error("httpfs static asset stat failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logging.MergeFields(r.Context(), logrus.Fields{
		"asset":    staticPath,
		"fallback": "index.html",
	})).Debug("httpfs static asset fallback served")
	r.URL.Path = "/index.html"
	fileServer.ServeHTTP(w, r)
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
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func writeSSEError(w http.ResponseWriter, message string) {
	_ = writeSSE(w, "runtime_error", APIError{Error: message})
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
			logrus.WithFields(fields).Error("httpfs request panicked")
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
			entry.Error("httpfs request completed")
		case status >= 400:
			entry.Warn("httpfs request completed")
		case duration >= slowRequestThreshold:
			entry.Info("httpfs request completed")
		default:
			entry.Debug("httpfs request completed")
		}
	})
}
