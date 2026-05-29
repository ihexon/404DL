package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"4dl/internal/logging"
	"4dl/internal/magnet"
	"4dl/internal/model"
)

type SearchRequest struct {
	Query     string
	Limit     int
	Providers []string
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

var ErrUnknownProvider = errors.New("unknown provider")

func (a *Aggregator) Search(ctx context.Context, req SearchRequest) ([]model.SearchResult, error) {
	providers, err := a.selectedProviders(req.Providers)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
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
		"providers":      len(providers),
		"provider_names": providerNames(providers),
	})).Info("provider aggregation started")

	type result struct {
		provider   string
		results    []model.SearchResult
		err        error
		durationMS int64
	}

	results := make(chan result, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
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
	merged = normalizeResults(merged)
	afterNormalize := len(merged)
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
		"normalized_results": afterNormalize,
		"items_removed":      rawCount - afterNormalize,
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

func (a *Aggregator) selectedProviders(requested []string) ([]Provider, error) {
	selected := normalizedProviderSet(requested)
	if len(selected) == 0 {
		return append([]Provider(nil), a.providers...), nil
	}

	out := make([]Provider, 0, len(selected))
	for _, p := range a.providers {
		name := strings.ToLower(strings.TrimSpace(p.Name()))
		if _, ok := selected[name]; !ok {
			continue
		}
		out = append(out, p)
		delete(selected, name)
	}

	if len(selected) > 0 {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownProvider, firstProviderName(selected), strings.Join(sortedProviderNames(a.providers), ", "))
	}
	return out, nil
}

func normalizedProviderSet(providers []string) map[string]struct{} {
	selected := make(map[string]struct{}, len(providers))
	for _, providerName := range providers {
		providerName = strings.ToLower(strings.TrimSpace(providerName))
		if providerName == "" {
			continue
		}
		selected[providerName] = struct{}{}
	}
	return selected
}

func firstProviderName(providers map[string]struct{}) string {
	names := make([]string, 0, len(providers))
	for providerName := range providers {
		names = append(names, providerName)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func sortedProviderNames(providers []Provider) []string {
	names := providerNames(providers)
	sort.Strings(names)
	return names
}

func normalizeResults(results []model.SearchResult) []model.SearchResult {
	positions := make(map[string]int, len(results))
	normalized := make([]model.SearchResult, 0, len(results))
	for _, result := range results {
		result = normalizeResult(result)
		key := dedupeKey(result)
		if key == "" {
			continue
		}
		pos, ok := positions[key]
		if ok {
			if result.Seeders > normalized[pos].Seeders {
				normalized[pos] = result
			}
			continue
		}
		positions[key] = len(normalized)
		normalized = append(normalized, result)
	}
	return normalized
}

func normalizeResult(result model.SearchResult) model.SearchResult {
	result.Hash = normalizeHashPtr(result.Hash)
	result.MagnetURL = magnet.NormalizeURLPtr(result.MagnetURL)
	return result
}

func normalizeHashPtr(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if normalized == "" {
		return nil
	}
	return &normalized
}

func dedupeKey(result model.SearchResult) string {
	if result.Hash != nil {
		return "hash:" + *result.Hash
	}
	return ""
}
