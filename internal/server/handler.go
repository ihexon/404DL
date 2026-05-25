package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"

	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type TorrentSearcher interface {
	Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error)
}

type Config struct {
	Addr       string
	PageSize   int
	HTTPClient *http.Client
}

type Handler struct {
	client   TorrentSearcher
	pageSize int
}

func NewHandler(client TorrentSearcher, cfg Config) *Handler {
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	if pageSize > 200 {
		pageSize = 200
	}

	return &Handler{
		client:   client,
		pageSize: pageSize,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /v1/t", h.searchTorrents)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) searchTorrents(w http.ResponseWriter, r *http.Request) {
	params := parseTorrentQuery(r)
	fields := log.Fields{
		"method":     r.Method,
		"path":       r.URL.Path,
		"query":      params.SearchName,
		"resolution": params.Resolution,
	}
	if strings.TrimSpace(params.SearchName) == "" {
		log.WithFields(fields).Info("rejecting request: missing search")
		writeError(w, http.StatusBadRequest, "search name is required")
		return
	}
	if strings.TrimSpace(params.Resolution) == "" {
		log.WithFields(fields).Info("rejecting request: missing resolution")
		writeError(w, http.StatusBadRequest, "resolution is required")
		return
	}
	if len(params.SearchName) > 200 {
		log.WithFields(fields).Info("rejecting request: search too long")
		writeError(w, http.StatusBadRequest, "search name is too long")
		return
	}

	log.WithFields(fields).Info("torrent search request received")
	hits, err := h.client.Search(r.Context(), provider.SearchRequest{
		Query:      params.SearchName,
		Resolution: params.Resolution,
		Limit:      h.pageSize,
	})
	if err != nil {
		log.WithError(err).WithFields(fields).Info("torrent search request failed")
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	log.WithFields(log.Fields{
		"query":      params.SearchName,
		"resolution": params.Resolution,
		"count":      len(hits),
	}).Info("torrent search request completed")
	writeJSON(w, http.StatusOK, hits)
}

type torrentPathParams struct {
	SearchName string
	Resolution string
}

func parseTorrentQuery(r *http.Request) torrentPathParams {
	query := r.URL.Query()
	return torrentPathParams{
		SearchName: strings.TrimSpace(query.Get("search")),
		Resolution: strings.TrimSpace(query.Get("resolution")),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
