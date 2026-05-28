package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
	"github.com/swaggest/swgui/v5emb"

	"4dl/internal/logging"
	"4dl/internal/model"
	"4dl/internal/provider"
	"4dl/internal/responsecodec"
)

type Config struct {
	Addr              string
	DefaultLimit      int
	HTTPClient        *http.Client
	ResponseEncryptor responsecodec.Encryptor
}

type Handler struct {
	client            Searcher
	defaultLimit      int
	responseEncryptor responsecodec.Encryptor
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

const (
	DefaultSearchLimit     = 50
	MaxSearchLimit         = 200
	MaxSearchQueryLength   = 200
	openAPISpecContentType = "application/vnd.oai.openapi+json; charset=utf-8"
)

func NewHandler(client Searcher, cfg Config) *Handler {
	return &Handler{
		client:            client,
		defaultLimit:      NormalizeLimit(cfg.DefaultLimit),
		responseEncryptor: cfg.ResponseEncryptor,
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
	mux.Handle("GET /docs/", v5emb.New("404 Downloader Search API", "/openapi.json", "/docs/"))
	mux.HandleFunc("GET /docs", redirectToDocs)
	mux.HandleFunc("GET /v1/search", h.searchFiles)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writePlainJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	requireEncrypted := responsecodec.RequiresEncryptedResponse(r.Header)

	if requireEncrypted && h.responseEncryptor == nil {
		fields := logging.HTTPRequestFields(r, requestID)
		fields["response_encryption_required"] = true
		logrus.WithFields(fields).Warn("search request rejected: encrypted response required but disabled")
		h.writeError(w, http.StatusPreconditionFailed, "encryption_required", "encrypted response is required but server response encryption is disabled", false)
		return
	}

	params, ok := h.parseSearchQuery(w, r, requestID)
	if !ok {
		return
	}

	fields := logging.HTTPRequestFields(r, requestID)
	fields["query"] = params.Query
	fields["limit"] = params.Limit
	fields["response_encryption_required"] = requireEncrypted
	if len(params.Providers) > 0 {
		fields["providers"] = params.Providers
	}

	logrus.WithFields(fields).Info("search request started")
	results, err := h.client.Search(ctx, provider.SearchRequest{
		Query:     params.Query,
		Limit:     params.Limit,
		Providers: params.Providers,
	})
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		if errors.Is(err, provider.ErrUnknownProvider) {
			logrus.WithError(err).WithFields(fields).Warn("search request rejected: unknown provider")
			h.writeError(w, http.StatusBadRequest, "bad_request", err.Error(), requireEncrypted)
			return
		}
		logrus.WithError(err).WithFields(fields).Error("search request failed")
		h.writeError(w, http.StatusBadGateway, "provider_error", err.Error(), requireEncrypted)
		return
	}
	encoded, err := h.responseBody(results, requireEncrypted)
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("search response encryption failed")
		h.writeError(w, http.StatusInternalServerError, "internal_server_error", "failed to encrypt search response", requireEncrypted)
		return
	}

	responsecodec.WriteHeaders(w.Header(), encoded)
	w.WriteHeader(http.StatusOK)
	written, err := w.Write(encoded.Body)
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("search response write failed")
		return
	}

	fields["status"] = http.StatusOK
	fields["result_count"] = len(results)
	fields["response_encryption"] = encoded.Encrypted
	fields["bytes"] = written
	fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
	logrus.WithFields(fields).Info("search request completed")
}

type searchParams struct {
	Query     string
	Limit     int
	Providers []string
}

func (h *Handler) parseSearchQuery(w http.ResponseWriter, r *http.Request, requestID string) (searchParams, bool) {
	query := r.URL.Query()
	params := searchParams{
		Query:     strings.TrimSpace(query.Get("q")),
		Limit:     h.defaultLimit,
		Providers: normalizedProviders(query["provider"]),
	}

	fields := logging.HTTPRequestFields(r, requestID)
	fields["query"] = params.Query

	if params.Query == "" {
		logrus.WithFields(fields).Warn("search request rejected: missing q")
		h.writeError(w, http.StatusBadRequest, "bad_request", "q is required", responsecodec.RequiresEncryptedResponse(r.Header))
		return searchParams{}, false
	}
	queryLength := utf8.RuneCountInString(params.Query)
	if queryLength > MaxSearchQueryLength {
		fields["query_length"] = queryLength
		logrus.WithFields(fields).Warn("search request rejected: q too long")
		h.writeError(w, http.StatusBadRequest, "bad_request", "q is too long", responsecodec.RequiresEncryptedResponse(r.Header))
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
		h.writeError(w, http.StatusBadRequest, "bad_request", "limit must be an integer", responsecodec.RequiresEncryptedResponse(r.Header))
		return searchParams{}, false
	}
	if limit < 1 || limit > MaxSearchLimit {
		fields["limit"] = limit
		logrus.WithFields(fields).Warn("search request rejected: limit out of range")
		h.writeError(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 200", responsecodec.RequiresEncryptedResponse(r.Header))
		return searchParams{}, false
	}
	params.Limit = limit
	return params, true
}

func normalizedProviders(providers []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(providers))
	for _, providerName := range providers {
		providerName = strings.ToLower(strings.TrimSpace(providerName))
		if providerName == "" {
			continue
		}
		if _, ok := seen[providerName]; ok {
			continue
		}
		seen[providerName] = struct{}{}
		out = append(out, providerName)
	}
	return out
}

func writePlainJSON(w http.ResponseWriter, status int, v any) {
	encoded, err := responsecodec.EncodeJSON(v, nil)
	if err != nil {
		logrus.WithError(err).Error("plain json response encode failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", encoded.ContentType)
	w.WriteHeader(status)
	if _, err := w.Write(encoded.Body); err != nil {
		logrus.WithError(err).Error("plain json response write failed")
	}
}

func (h *Handler) responseBody(v any, encrypt bool) (responsecodec.EncodedBody, error) {
	if !encrypt {
		return responsecodec.EncodeJSON(v, nil)
	}
	return responsecodec.EncodeJSON(v, h.responseEncryptor)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, msg string, encrypt bool) {
	encoded, err := h.responseBody(errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: msg,
		},
	}, encrypt)
	if err != nil {
		logrus.WithError(err).Error("error response encryption failed")
		writePlainJSON(w, status, errorResponse{
			Error: errorDetail{
				Code:    "internal_server_error",
				Message: "failed to encrypt error response",
			},
		})
		return
	}

	responsecodec.WriteHeaders(w.Header(), encoded)
	w.WriteHeader(status)
	if _, err := w.Write(encoded.Body); err != nil {
		logrus.WithError(err).Error("error response write failed")
	}
}
