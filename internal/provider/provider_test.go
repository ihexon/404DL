package provider

import (
	"context"
	"testing"

	"mvdl/internal/model"
)

type stubProvider struct {
	name    string
	results []model.Torrent
}

func (p stubProvider) Name() string {
	return p.name
}

func (p stubProvider) Search(context.Context, SearchRequest) ([]model.Torrent, error) {
	return p.results, nil
}

func TestNewAggregatorCopiesProviderSlice(t *testing.T) {
	providers := []Provider{
		stubProvider{name: "one", results: []model.Torrent{{Provider: "one", Title: "one 1080p", Seeders: 1}}},
	}
	aggregator := NewAggregator(providers...)
	providers[0] = stubProvider{name: "two", results: []model.Torrent{{Provider: "two", Title: "two 1080p", Seeders: 2}}}

	got, err := aggregator.Search(context.Background(), SearchRequest{Resolution: "1080p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Provider != "one" {
		t.Fatalf("Search() = %+v, want provider one", got)
	}
}
