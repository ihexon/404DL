package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

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
	log.WithFields(log.Fields{
		"query":      req.Query,
		"resolution": req.Resolution,
		"limit":      req.Limit,
		"providers":  len(a.providers),
	}).Info("provider aggregation started")

	type result struct {
		provider string
		torrents []model.Torrent
		err      error
	}

	results := make(chan result, len(a.providers))
	var wg sync.WaitGroup
	for _, p := range a.providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			log.WithFields(log.Fields{
				"provider":   p.Name(),
				"query":      req.Query,
				"resolution": req.Resolution,
				"limit":      req.Limit,
			}).Info("provider search started")
			torrents, err := p.Search(ctx, req)
			if err != nil {
				log.WithError(err).WithField("provider", p.Name()).Info("provider search failed")
				results <- result{provider: p.Name(), err: fmt.Errorf("%s: %w", p.Name(), err)}
				return
			}
			log.WithFields(log.Fields{
				"provider": p.Name(),
				"count":    len(torrents),
			}).Info("provider search completed")
			results <- result{provider: p.Name(), torrents: torrents}
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

	beforeFilter := len(merged)
	merged = filterByResolution(merged, req.Resolution)
	afterFilter := len(merged)
	merged = dedupe(merged)
	afterDedupe := len(merged)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Seeders > merged[j].Seeders
	})
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
	}

	log.WithFields(log.Fields{
		"query":         req.Query,
		"resolution":    req.Resolution,
		"before_filter": beforeFilter,
		"after_filter":  afterFilter,
		"after_dedupe":  afterDedupe,
		"returned":      len(merged),
		"errors":        len(errs),
	}).Info("provider aggregation completed")
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
