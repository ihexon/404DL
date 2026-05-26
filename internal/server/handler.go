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

type Config struct {
	Addr            string
	PageSize        int
	HTTPClient      *http.Client
	MagnetEncryptor StringEncryptor
}

type Handler struct {
	client          TorrentSearcher
	pageSize        int
	magnetEncryptor StringEncryptor
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TorrentSearcher interface {
	Search(context.Context, provider.SearchRequest) ([]model.Torrent, error)
}

type StringEncryptor interface {
	EncryptString(plaintext string) (string, error)
}

func NewHandler(client TorrentSearcher, cfg Config) *Handler {
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}

	if pageSize > 200 {
		pageSize = 200
	}

	return &Handler{
		client:          client,
		pageSize:        pageSize,
		magnetEncryptor: cfg.MagnetEncryptor,
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
		"method": r.Method,
		"path":   r.URL.Path,
		"query":  params.SearchName,
		"filter": params.Filter,
	}
	if strings.TrimSpace(params.SearchName) == "" {
		log.WithFields(fields).Info("rejecting request: missing search")
		writeError(w, http.StatusBadRequest, "bad_request", "search name is required")
		return
	}
	if len(params.SearchName) > 200 {
		log.WithFields(fields).Info("rejecting request: search too long")
		writeError(w, http.StatusBadRequest, "bad_request", "search name is too long")
		return
	}

	log.WithFields(fields).Info("torrent search request received")
	hits, err := h.client.Search(r.Context(), provider.SearchRequest{
		Query:  params.SearchName,
		Filter: params.Filter,
		Limit:  h.pageSize,
	})
	if err != nil {
		log.WithError(err).WithFields(fields).Info("torrent search request failed")
		writeError(w, http.StatusBadGateway, "provider_error", err.Error())
		return
	}
	encryptMagnetURL := h.magnetEncryptor != nil
	if encryptMagnetURL {
		encrypted, err := h.encryptMagnets(hits)
		if err != nil {
			log.WithError(err).WithFields(fields).Info("torrent magnet encryption failed")
			writeError(w, http.StatusInternalServerError, "internal_server_error", "failed to encrypt magnetUrl")
			return
		}
		hits = encrypted
	}

	log.WithFields(log.Fields{
		"query":     params.SearchName,
		"filter":    params.Filter,
		"count":     len(hits),
		"encrypted": encryptMagnetURL,
	}).Info("torrent search request completed")
	writeJSON(w, http.StatusOK, hits)
}

func (h *Handler) encryptMagnets(hits []model.Torrent) ([]model.Torrent, error) {
	encrypted := make([]model.Torrent, len(hits))
	copy(encrypted, hits)

	count := 0
	for i := range encrypted {
		if encrypted[i].MagnetURL == nil || *encrypted[i].MagnetURL == "" {
			continue
		}
		value, err := h.magnetEncryptor.EncryptString(*encrypted[i].MagnetURL)
		if err != nil {
			return nil, err
		}
		encrypted[i].MagnetURL = &value
		count++
	}

	log.WithField("count", count).Info("magnetUrl fields encrypted")
	return encrypted, nil
}

type torrentPathParams struct {
	SearchName string
	Filter     string
}

func parseTorrentQuery(r *http.Request) torrentPathParams {
	query := r.URL.Query()
	return torrentPathParams{
		SearchName: strings.TrimSpace(query.Get("search")),
		Filter:     strings.TrimSpace(query.Get("filter")),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: msg,
		},
	})
}
