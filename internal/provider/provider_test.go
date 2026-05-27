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

	got, err := aggregator.Search(context.Background(), SearchRequest{Query: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Provider != "one" {
		t.Fatalf("Search() = %+v, want provider one", got)
	}
}

func TestAggregatorReturnsProviderResults(t *testing.T) {
	aggregator := NewAggregator(stubProvider{
		name: "one",
		results: []model.Torrent{
			{Provider: "one", Title: "Ubuntu Linux ISO", Seeders: 10},
			{Provider: "one", Title: "movie", Resolution: "1080p", Seeders: 8},
		},
	})

	got, err := aggregator.Search(context.Background(), SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Search() returned %d results, want 2", len(got))
	}
}
