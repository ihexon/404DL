package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"

	"mvdl/internal/model"
)

type SearchRequest struct {
	Query string
	Limit int
}

type Provider interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) ([]model.Torrent, error)
}

type Aggregator struct {
	providers []Provider
}

func NewAggregator(providers ...Provider) *Aggregator {
	return &Aggregator{providers: append([]Provider(nil), providers...)}
}

func (a *Aggregator) Search(ctx context.Context, req SearchRequest) ([]model.Torrent, error) {
	if len(a.providers) == 0 {
		return nil, errors.New("no providers configured")
	}

	log.WithFields(log.Fields{
		"query":     req.Query,
		"limit":     req.Limit,
		"providers": len(a.providers),
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
				"provider": p.Name(),
				"query":    req.Query,
				"limit":    req.Limit,
			}).Info("provider search started")
			torrents, err := p.Search(ctx, req)
			if err != nil {
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
		merged          []model.Torrent
		errs            []error
		okProviders     []string
		failedProviders []string
	)
	for res := range results {
		if res.err != nil {
			fields := ErrorFields(res.err)
			fields["provider"] = res.provider
			log.WithFields(fields).Warn("provider search failed")
			errs = append(errs, res.err)
			failedProviders = append(failedProviders, res.provider)
			continue
		}
		okProviders = append(okProviders, res.provider)
		merged = append(merged, res.torrents...)
	}

	if len(merged) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	merged = dedupe(merged)
	afterDedupe := len(merged)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Seeders > merged[j].Seeders
	})
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
	}
	merged = append([]model.Torrent(nil), merged...)

	log.WithFields(log.Fields{
		"query":         req.Query,
		"after_dedupe":  afterDedupe,
		"returned":      len(merged),
		"errors":        len(errs),
		"providers_ok":  okProviders,
		"providers_err": failedProviders,
	}).Info("provider aggregation completed")
	return merged, nil
}

func dedupe(torrents []model.Torrent) []model.Torrent {
	positions := make(map[string]int, len(torrents))
	deduped := make([]model.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		key := dedupeKey(torrent)
		if key == "" {
			deduped = append(deduped, torrent)
			continue
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
	if torrent.Hash != nil {
		hash := strings.TrimSpace(*torrent.Hash)
		if hash != "" {
			return "hash:" + strings.ToLower(hash)
		}
	}
	if torrent.MagnetURL != nil {
		magnetURL := strings.TrimSpace(*torrent.MagnetURL)
		if magnetURL != "" {
			return "magnet:" + strings.ToLower(magnetURL)
		}
	}
	title := strings.ToLower(strings.TrimSpace(torrent.Title))
	if title == "" {
		return ""
	}
	parts := []string{
		"torrent",
		strings.ToLower(strings.TrimSpace(torrent.Provider)),
		title,
	}
	if torrent.Bytes > 0 {
		parts = append(parts, strconv.FormatInt(torrent.Bytes, 10))
	}
	return strings.Join(parts, ":")
}
