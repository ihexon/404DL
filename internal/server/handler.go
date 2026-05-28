package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
	"github.com/swaggest/swgui/v5emb"

	"mvdl/internal/logging"
	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type Config struct {
	Addr            string
	DefaultLimit    int
	HTTPClient      *http.Client
	MagnetEncryptor StringEncryptor
}

type Handler struct {
	client          Searcher
	defaultLimit    int
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

const (
	DefaultSearchLimit     = 50
	MaxSearchLimit         = 200
	MaxSearchQueryLength   = 200
	openAPISpecContentType = "application/vnd.oai.openapi+json; charset=utf-8"
)

func NewHandler(client Searcher, cfg Config) *Handler {
	return &Handler{
		client:          client,
		defaultLimit:    NormalizeLimit(cfg.DefaultLimit),
		magnetEncryptor: cfg.MagnetEncryptor,
	}
}

func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		return MaxSearchLimit
	}
	return limit
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /openapi.json", h.openAPI)
	mux.Handle("GET /docs/", v5emb.New("mvdl Search API", "/openapi.json", "/docs/"))
	mux.HandleFunc("GET /docs", redirectToDocs)
	mux.HandleFunc("GET /v1/search", h.searchFiles)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) openAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", openAPISpecContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpec)
}

func redirectToDocs(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
}

func (h *Handler) searchFiles(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	requestID := logging.RequestIDFromHTTP(r)
	w.Header().Set(logging.RequestIDHeader, requestID)
	ctx := logging.WithRequestID(r.Context(), requestID)

	params, ok := h.parseSearchQuery(w, r, requestID)
	if !ok {
		return
	}

	fields := logging.HTTPRequestFields(r, requestID)
	fields["query"] = params.Query
	fields["limit"] = params.Limit

	logrus.WithFields(fields).Info("search request started")
	results, err := h.client.Search(ctx, provider.SearchRequest{
		Query: params.Query,
		Limit: params.Limit,
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
	Limit int
}

func (h *Handler) parseSearchQuery(w http.ResponseWriter, r *http.Request, requestID string) (searchParams, bool) {
	query := r.URL.Query()
	params := searchParams{
		Query: strings.TrimSpace(query.Get("q")),
		Limit: h.defaultLimit,
	}

	fields := logging.HTTPRequestFields(r, requestID)
	fields["query"] = params.Query

	if params.Query == "" {
		logrus.WithFields(fields).Warn("search request rejected: missing q")
		writeError(w, http.StatusBadRequest, "bad_request", "q is required")
		return searchParams{}, false
	}
	queryLength := utf8.RuneCountInString(params.Query)
	if queryLength > MaxSearchQueryLength {
		fields["query_length"] = queryLength
		logrus.WithFields(fields).Warn("search request rejected: q too long")
		writeError(w, http.StatusBadRequest, "bad_request", "q is too long")
		return searchParams{}, false
	}

	rawLimit := strings.TrimSpace(query.Get("limit"))
	if rawLimit == "" {
		return params, true
	}
	limit, err := strconv.Atoi(rawLimit)
	if err != nil {
		fields["limit"] = rawLimit
		logrus.WithFields(fields).Warn("search request rejected: invalid limit")
		writeError(w, http.StatusBadRequest, "bad_request", "limit must be an integer")
		return searchParams{}, false
	}
	if limit < 1 || limit > MaxSearchLimit {
		fields["limit"] = limit
		logrus.WithFields(fields).Warn("search request rejected: limit out of range")
		writeError(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 200")
		return searchParams{}, false
	}
	params.Limit = limit
	return params, true
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
