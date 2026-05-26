package knaben

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mvdl/internal/provider"
)

func TestSearchOmitsPaginationAndSortsBySeeders(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())
	_, err := client.Search(context.Background(), provider.SearchRequest{
		Query:  "movie",
		Filter: "1080p",
		Limit:  1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := body["from"]; ok {
		t.Fatalf("request contains from: %#v", body["from"])
	}
	if _, ok := body["size"]; ok {
		t.Fatalf("request contains size: %#v", body["size"])
	}
	if got := body["order_by"]; got != "seeders" {
		t.Fatalf("order_by = %v, want seeders", got)
	}
	if got := body["order_direction"]; got != "desc" {
		t.Fatalf("order_direction = %v, want desc", got)
	}
}
