package httpfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	defaultTorrentListenAddr = ":42069"
	minDownloadReadahead     = 16 * 1024 * 1024
	maxDownloadReadahead     = 256 * 1024 * 1024
)

func Run(ctx context.Context, cfg Config) error {
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

	log.Printf("httpfs web ui: http://%s", listener.Addr().String())
	httpServer := &http.Server{Handler: server.routes()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
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
	mux.HandleFunc("GET /d/{id}/{filePath...}", s.handleDownload)
	mux.HandleFunc("GET /", s.handleStatic)
	return mux
}

func (s *Server) handleListTorrents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.manager.List())
}

func (s *Server) handleGetTorrent(w http.ResponseWriter, r *http.Request) {
	item, ok := s.manager.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleGetFiles(w http.ResponseWriter, r *http.Request) {
	item, ok := s.manager.EnsureFiles(r.Context(), r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, APIError{Error: "torrent not found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	filePath := r.PathValue("filePath")
	if filePath == "" {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	t, ok, err := s.manager.FileTorrent(r.Context(), id)
	if !ok {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}

	for _, file := range t.Files() {
		if file.DisplayPath() != filePath {
			continue
		}
		reader := file.NewReader()
		reader.SetContext(r.Context())
		reader.SetResponsive()
		reader.SetReadaheadFunc(downloadReadahead)
		defer reader.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", downloadETag(t.InfoHash().HexString(), filePath, file.Length()))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
			"filename": path.Base(filePath),
		}))
		http.ServeContent(w, r, path.Base(filePath), time.Time{}, reader)
		return
	}
	http.Error(w, "file not found", http.StatusNotFound)
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
		fileServer.ServeHTTP(w, r)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
