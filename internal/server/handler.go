package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"mvdl/internal/logging"
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
	startedAt := time.Now()
	requestID := logging.RequestIDFromHTTP(r)
	w.Header().Set(logging.RequestIDHeader, requestID)
	ctx := logging.WithRequestID(r.Context(), requestID)

	params := parseTorrentQuery(r)
	fields := logging.HTTPRequestFields(r, requestID)
	fields["query"] = params.SearchName
	fields["page_size"] = h.pageSize

	if strings.TrimSpace(params.SearchName) == "" {
		logrus.WithFields(fields).Warn("torrent search request rejected: missing search")
		writeError(w, http.StatusBadRequest, "bad_request", "search name is required")
		return
	}
	if len(params.SearchName) > 200 {
		fields["query_length"] = len(params.SearchName)
		logrus.WithFields(fields).Warn("torrent search request rejected: search too long")
		writeError(w, http.StatusBadRequest, "bad_request", "search name is too long")
		return
	}

	logrus.WithFields(fields).Info("torrent search request started")
	hits, err := h.client.Search(ctx, provider.SearchRequest{
		Query: params.SearchName,
		Limit: h.pageSize,
	})
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("torrent search request failed")
		writeError(w, http.StatusBadGateway, "provider_error", err.Error())
		return
	}
	encryptMagnetURL := h.magnetEncryptor != nil
	if encryptMagnetURL {
		encrypted, encryptedCount, err := h.encryptMagnets(hits)
		if err != nil {
			fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
			logrus.WithError(err).WithFields(fields).Error("torrent magnet encryption failed")
			writeError(w, http.StatusInternalServerError, "internal_server_error", "failed to encrypt magnetUrl")
			return
		}
		hits = encrypted
		fields["encrypted_magnets"] = encryptedCount
	}

	fields["status"] = http.StatusOK
	fields["result_count"] = len(hits)
	fields["magnet_encryption"] = encryptMagnetURL
	fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
	logrus.WithFields(fields).Info("torrent search request completed")
	writeJSON(w, http.StatusOK, hits)
}

func (h *Handler) encryptMagnets(hits []model.Torrent) ([]model.Torrent, int, error) {
	encrypted := make([]model.Torrent, len(hits))
	copy(encrypted, hits)

	count := 0
	for i := range encrypted {
		if encrypted[i].MagnetURL == nil || *encrypted[i].MagnetURL == "" {
			continue
		}
		value, err := h.magnetEncryptor.EncryptString(*encrypted[i].MagnetURL)
		if err != nil {
			return nil, count, err
		}
		encrypted[i].MagnetURL = &value
		count++
	}

	return encrypted, count, nil
}

type torrentPathParams struct {
	SearchName string
}

func parseTorrentQuery(r *http.Request) torrentPathParams {
	query := r.URL.Query()
	return torrentPathParams{
		SearchName: strings.TrimSpace(query.Get("search")),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: msg,
		},
	})
}
