package downloader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

func serveStatus(ctx context.Context, client *torrent.Client, addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen status server: %w", err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: statusHandler(client),
	}

	go shutdownStatusServer(ctx, server)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			_, _ = fmt.Fprintf(io.Discard, "status server stopped: %v", err)
		}
	}()
	return nil
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
