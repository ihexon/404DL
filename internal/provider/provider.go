package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"mvdl/internal/logging"
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
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"query": req.Query,
			"limit": req.Limit,
		})).Error("provider aggregation failed: no providers configured")
		return nil, errors.New("no providers configured")
	}

	startedAt := time.Now()
	logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
		"query":          req.Query,
		"limit":          req.Limit,
		"providers":      len(a.providers),
		"provider_names": providerNames(a.providers),
	})).Info("provider aggregation started")

	type result struct {
		provider   string
		torrents   []model.Torrent
		err        error
		durationMS int64
	}

	results := make(chan result, len(a.providers))
	var wg sync.WaitGroup
	for _, p := range a.providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			providerStartedAt := time.Now()
			providerFields := logging.MergeFields(ctx, logrus.Fields{
				"provider": p.Name(),
				"query":    req.Query,
				"limit":    req.Limit,
			})
			logrus.WithFields(providerFields).Info("provider search started")

			torrents, err := p.Search(ctx, req)
			durationMS := logging.DurationMillis(time.Since(providerStartedAt))
			if err != nil {
				results <- result{provider: p.Name(), err: fmt.Errorf("%s: %w", p.Name(), err), durationMS: durationMS}
				return
			}
			providerFields["result_count"] = len(torrents)
			providerFields["duration_ms"] = durationMS
			logrus.WithFields(providerFields).Info("provider search completed")
			results <- result{provider: p.Name(), torrents: torrents, durationMS: durationMS}
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
			for key, value := range logging.ContextFields(ctx) {
				fields[key] = value
			}
			fields["provider"] = res.provider
			fields["duration_ms"] = res.durationMS
			logrus.WithFields(fields).Warn("provider search failed")
			errs = append(errs, res.err)
			failedProviders = append(failedProviders, res.provider)
			continue
		}
		okProviders = append(okProviders, res.provider)
		merged = append(merged, res.torrents...)
	}

	if len(merged) == 0 && len(errs) > 0 {
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"query":         req.Query,
			"limit":         req.Limit,
			"errors":        len(errs),
			"providers_err": failedProviders,
			"duration_ms":   logging.DurationMillis(time.Since(startedAt)),
		})).Error("provider aggregation failed: all providers failed")
		return nil, errors.Join(errs...)
	}

	rawCount := len(merged)
	merged = dedupe(merged)
	afterDedupe := len(merged)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Seeders > merged[j].Seeders
	})
	limited := false
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
		limited = true
	}
	merged = append([]model.Torrent(nil), merged...)

	logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
		"query":              req.Query,
		"limit":              req.Limit,
		"raw_results":        rawCount,
		"deduped_results":    afterDedupe,
		"duplicates_removed": rawCount - afterDedupe,
		"returned":           len(merged),
		"limit_applied":      limited,
		"errors":             len(errs),
		"providers_ok":       okProviders,
		"providers_err":      failedProviders,
		"duration_ms":        logging.DurationMillis(time.Since(startedAt)),
	})).Info("provider aggregation completed")
	return merged, nil
}

func providerNames(providers []Provider) []string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name())
	}
	return names
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
