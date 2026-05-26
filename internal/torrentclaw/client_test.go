package torrentclaw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"mvdl/internal/provider"
)

func TestSearchOmitsPaginationAndSortsBySeeders(t *testing.T) {
	var values url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())
	_, err := client.Search(context.Background(), provider.SearchRequest{
		Query:      "movie",
		Resolution: "1080p",
		Limit:      1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := values.Get("page"); got != "" {
		t.Fatalf("page = %q, want empty", got)
	}
	if got := values.Get("limit"); got != "" {
		t.Fatalf("limit = %q, want empty", got)
	}
	if got := values.Get("sort"); got != "seeders" {
		t.Fatalf("sort = %q, want seeders", got)
	}
	if got := values.Get("quality"); got != "1080p" {
		t.Fatalf("quality = %q, want 1080p", got)
	}
}
