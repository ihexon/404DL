package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type stubEncryptor struct{}

func (stubEncryptor) EncryptString(plaintext string) (string, error) {
	return "encrypted:" + plaintext, nil
}

func TestEncryptMagnetsDoesNotMutateInput(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:abc"
	h := NewHandler(nil, Config{MagnetEncryptor: stubEncryptor{}})
	hits := []model.Torrent{{Title: "movie", MagnetURL: &magnet}}

	encrypted, err := h.encryptMagnets(hits)
	if err != nil {
		t.Fatal(err)
	}
	if got := *hits[0].MagnetURL; got != magnet {
		t.Fatalf("input magnet = %q, want %q", got, magnet)
	}
	if got := *encrypted[0].MagnetURL; !strings.HasPrefix(got, "encrypted:") {
		t.Fatalf("encrypted magnet = %q, want encrypted prefix", got)
	}
}

type stubSearcher struct {
	requests []provider.SearchRequest
}

func (s *stubSearcher) Search(_ context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	s.requests = append(s.requests, req)
	return []model.Torrent{{Provider: "test", Title: "ubuntu"}}, nil
}

func TestSearchTorrentsAllowsMissingFilter(t *testing.T) {
	searcher := &stubSearcher{}
	handler := NewHandler(searcher, Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/t?search=ubuntu", nil)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(searcher.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(searcher.requests))
	}
	if got := searcher.requests[0].Filter; got != "" {
		t.Fatalf("filter = %q, want empty", got)
	}
}

func TestSearchTorrentsPassesFilter(t *testing.T) {
	searcher := &stubSearcher{}
	handler := NewHandler(searcher, Config{})
	req := httptest.NewRequest(http.MethodGet, "/v1/t?search=movie&filter=1080P", nil)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(searcher.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(searcher.requests))
	}
	if got := searcher.requests[0].Filter; got != "1080P" {
		t.Fatalf("filter = %q, want 1080P", got)
	}
}
