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
	client          Searcher
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

type Searcher interface {
	Search(context.Context, provider.SearchRequest) ([]model.SearchResult, error)
}

type StringEncryptor interface {
	EncryptString(plaintext string) (string, error)
}

func NewHandler(client Searcher, cfg Config) *Handler {
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
	mux.HandleFunc("GET /v1/search", h.searchFiles)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) searchFiles(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	requestID := logging.RequestIDFromHTTP(r)
	w.Header().Set(logging.RequestIDHeader, requestID)
	ctx := logging.WithRequestID(r.Context(), requestID)

	params := parseSearchQuery(r)
	fields := logging.HTTPRequestFields(r, requestID)
	fields["query"] = params.Query
	fields["page_size"] = h.pageSize

	if strings.TrimSpace(params.Query) == "" {
		logrus.WithFields(fields).Warn("search request rejected: missing query")
		writeError(w, http.StatusBadRequest, "bad_request", "search query is required")
		return
	}
	if len(params.Query) > 200 {
		fields["query_length"] = len(params.Query)
		logrus.WithFields(fields).Warn("search request rejected: search too long")
		writeError(w, http.StatusBadRequest, "bad_request", "search query is too long")
		return
	}

	logrus.WithFields(fields).Info("search request started")
	results, err := h.client.Search(ctx, provider.SearchRequest{
		Query: params.Query,
		Limit: h.pageSize,
	})
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("search request failed")
		writeError(w, http.StatusBadGateway, "provider_error", err.Error())
		return
	}
	encryptMagnetURL := h.magnetEncryptor != nil
	if encryptMagnetURL {
		encrypted, encryptedCount, err := EncryptMagnetURLs(results, h.magnetEncryptor)
		if err != nil {
			fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
			logrus.WithError(err).WithFields(fields).Error("magnet URL encryption failed")
			writeError(w, http.StatusInternalServerError, "internal_server_error", "failed to encrypt magnetUrl")
			return
		}
		results = encrypted
		fields["encrypted_magnets"] = encryptedCount
	}

	fields["status"] = http.StatusOK
	fields["result_count"] = len(results)
	fields["magnet_encryption"] = encryptMagnetURL
	fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
	logrus.WithFields(fields).Info("search request completed")
	writeJSON(w, http.StatusOK, results)
}

func EncryptMagnetURLs(results []model.SearchResult, encryptor StringEncryptor) ([]model.SearchResult, int, error) {
	encrypted := make([]model.SearchResult, len(results))
	copy(encrypted, results)

	count := 0
	for i := range encrypted {
		if encrypted[i].MagnetURL == nil || *encrypted[i].MagnetURL == "" {
			continue
		}
		value, err := encryptor.EncryptString(*encrypted[i].MagnetURL)
		if err != nil {
			return nil, count, err
		}
		encrypted[i].MagnetURL = &value
		count++
	}

	return encrypted, count, nil
}

type searchParams struct {
	Query string
}

func parseSearchQuery(r *http.Request) searchParams {
	query := r.URL.Query()
	return searchParams{
		Query: strings.TrimSpace(query.Get("q")),
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
