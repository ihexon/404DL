package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"mvdl/internal/model"
)

type SearchRequest struct {
	Query      string
	Resolution string
	Limit      int
}

type Provider interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) ([]model.Torrent, error)
}

type Aggregator struct {
	providers []Provider
}

func NewAggregator(providers ...Provider) *Aggregator {
	return &Aggregator{providers: providers}
}

func (a *Aggregator) Search(ctx context.Context, req SearchRequest) ([]model.Torrent, error) {
	if len(a.providers) == 0 {
		return nil, errors.New("no providers configured")
	}

	type result struct {
		torrents []model.Torrent
		err      error
	}

	results := make(chan result, len(a.providers))
	var wg sync.WaitGroup
	for _, p := range a.providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			torrents, err := p.Search(ctx, req)
			if err != nil {
				results <- result{err: fmt.Errorf("%s: %w", p.Name(), err)}
				return
			}
			results <- result{torrents: torrents}
		}(p)
	}

	wg.Wait()
	close(results)

	var (
		merged []model.Torrent
		errs   []error
	)
	for res := range results {
		if res.err != nil {
			errs = append(errs, res.err)
			continue
		}
		merged = append(merged, res.torrents...)
	}

	if len(merged) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	merged = filterByResolution(merged, req.Resolution)
	merged = dedupe(merged)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Seeders > merged[j].Seeders
	})
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
	}

	return merged, nil
}

func filterByResolution(torrents []model.Torrent, resolution string) []model.Torrent {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	if resolution == "" {
		return torrents
	}

	filtered := make([]model.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if strings.EqualFold(torrent.Resolution, resolution) || strings.Contains(strings.ToLower(torrent.Title), resolution) {
			filtered = append(filtered, torrent)
		}
	}
	return filtered
}

func dedupe(torrents []model.Torrent) []model.Torrent {
	positions := make(map[string]int, len(torrents))
	deduped := make([]model.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		key := dedupeKey(torrent)
		if key == "" {
			key = torrent.Provider + ":" + strings.ToLower(torrent.Title)
		}
		pos, ok := positions[key]
		if ok {
			if torrent.Seeders > deduped[pos].Seeders {
				deduped[pos] = torrent
			}
			continue
		}
		positions[key] = len(deduped)
		deduped = append(deduped, torrent)
	}
	return deduped
}

func dedupeKey(torrent model.Torrent) string {
	if torrent.Hash != nil && *torrent.Hash != "" {
		return "hash:" + strings.ToLower(*torrent.Hash)
	}
	if torrent.MagnetURL != nil && *torrent.MagnetURL != "" {
		return "magnet:" + strings.ToLower(*torrent.MagnetURL)
	}
	if torrent.ID != "" {
		return torrent.Provider + ":id:" + strings.ToLower(torrent.ID)
	}
	return ""
}
