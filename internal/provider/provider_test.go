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

	got, err := aggregator.Search(context.Background(), SearchRequest{Filter: "1080p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Provider != "one" {
		t.Fatalf("Search() = %+v, want provider one", got)
	}
}

func TestAggregatorFiltersCaseInsensitive(t *testing.T) {
	aggregator := NewAggregator(stubProvider{
		name: "one",
		results: []model.Torrent{
			{Provider: "one", Title: "Ubuntu Linux ISO", Seeders: 10},
			{Provider: "one", Title: "movie", Resolution: "1080p", Seeders: 8},
			{Provider: "one", Title: "book", Category: "EBooks", Seeders: 6},
			{Provider: "one", Title: "other", Seeders: 4},
		},
	})

	got, err := aggregator.Search(context.Background(), SearchRequest{Filter: "LINUX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Ubuntu Linux ISO" {
		t.Fatalf("Search() = %+v, want Ubuntu Linux ISO only", got)
	}
}

func TestAggregatorDoesNotFilterWhenFilterIsEmpty(t *testing.T) {
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
