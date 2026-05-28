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
	Search(ctx context.Context, req SearchRequest) ([]model.SearchResult, error)
}

type Aggregator struct {
	providers []Provider
}

func NewAggregator(providers ...Provider) *Aggregator {
	return &Aggregator{providers: append([]Provider(nil), providers...)}
}

func (a *Aggregator) Search(ctx context.Context, req SearchRequest) ([]model.SearchResult, error) {
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
		results    []model.SearchResult
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

			searchResults, err := p.Search(ctx, req)
			durationMS := logging.DurationMillis(time.Since(providerStartedAt))
			if err != nil {
				results <- result{provider: p.Name(), err: fmt.Errorf("%s: %w", p.Name(), err), durationMS: durationMS}
				return
			}
			providerFields["result_count"] = len(searchResults)
			providerFields["duration_ms"] = durationMS
			logrus.WithFields(providerFields).Info("provider search completed")
			results <- result{provider: p.Name(), results: searchResults, durationMS: durationMS}
		}(p)
	}

	wg.Wait()
	close(results)

	var (
		merged          []model.SearchResult
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
		merged = append(merged, res.results...)
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
	merged = append([]model.SearchResult(nil), merged...)

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

func dedupe(results []model.SearchResult) []model.SearchResult {
	positions := make(map[string]int, len(results))
	deduped := make([]model.SearchResult, 0, len(results))
	for _, result := range results {
		key := dedupeKey(result)
		if key == "" {
			deduped = append(deduped, result)
			continue
		}
		pos, ok := positions[key]
		if ok {
			if result.Seeders > deduped[pos].Seeders {
				deduped[pos] = result
			}
			continue
		}
		positions[key] = len(deduped)
		deduped = append(deduped, result)
	}
	return deduped
}

func dedupeKey(result model.SearchResult) string {
	if result.Hash != nil {
		hash := strings.TrimSpace(*result.Hash)
		if hash != "" {
			return "hash:" + strings.ToLower(hash)
		}
	}
	if result.MagnetURL != nil {
		magnetURL := strings.TrimSpace(*result.MagnetURL)
		if magnetURL != "" {
			return "magnet:" + strings.ToLower(magnetURL)
		}
	}
	title := strings.ToLower(strings.TrimSpace(result.Title))
	if title == "" {
		return ""
	}
	parts := []string{
		"result",
		strings.ToLower(strings.TrimSpace(result.Provider)),
		title,
	}
	if result.Bytes > 0 {
		parts = append(parts, strconv.FormatInt(result.Bytes, 10))
	}
	return strings.Join(parts, ":")
}
