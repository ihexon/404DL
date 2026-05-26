package downloader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	log "github.com/sirupsen/logrus"
)

type statusServer struct {
	server *http.Server
	done   chan struct{}
}

func serveStatus(ctx context.Context, client *torrent.Client, addr string) (*statusServer, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen status server: %w", err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: statusHandler(client),
	}
	status := &statusServer{
		server: server,
		done:   make(chan struct{}),
	}

	go shutdownStatusServer(ctx, server)
	go func() {
		defer close(status.done)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Warn("status server stopped")
		}
	}()
	return status, nil
}

func (s *statusServer) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown status server: %w", err)
	}

	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func statusHandler(client *torrent.Client) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", writeTorrentStatus(client))
	mux.HandleFunc("GET /status", writeTorrentStatus(client))
	return mux
}

func writeTorrentStatus(client *torrent.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		client.WriteStatus(w)
	}
}

func shutdownStatusServer(ctx context.Context, server *http.Server) {
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
