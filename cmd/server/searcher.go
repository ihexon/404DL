package main

import (
	"fmt"
	"net/http"
	"strings"

	"mvdl/internal/knaben"
	"mvdl/internal/provider"
	"mvdl/internal/search"
	"mvdl/internal/torrentclaw"
)

type providerFactory struct {
	name string
	new  func(*http.Client) provider.Provider
}

var providerFactories = []providerFactory{
	{
		name: "knaben",
		new: func(client *http.Client) provider.Provider {
			return knaben.NewClient(envString(envKnabenAPIURL, defaultKnabenAPIURL), client)
		},
	},
	{
		name: "torrentclaw",
		new: func(client *http.Client) provider.Provider {
			return torrentclaw.NewClient(
				envString(envTorrentClawAPIURL, defaultTorrentClawURL),
				client,
				torrentclaw.WithAPIKey(envString(envTorrentClawAPIKey, "")),
			)
		},
	},
}

func newTorrentSearcher(client *http.Client, providerNames ...string) (*search.Service, error) {
	providers, err := newSearchProviders(client, providerNames...)
	if err != nil {
		return nil, err
	}

	aggregator := provider.NewAggregator(providers...)
	return search.NewService(newMetadataResolver(client), aggregator), nil
}

func newSearchProviders(client *http.Client, providerNames ...string) ([]provider.Provider, error) {
	selected := selectedProviderNames(providerNames)
	filtered := len(selected) > 0
	out := make([]provider.Provider, 0, len(providerFactories))

	for _, factory := range providerFactories {
		if !filtered {
			out = append(out, factory.new(client))
			continue
		}
		if _, ok := selected[factory.name]; ok {
			out = append(out, factory.new(client))
			delete(selected, factory.name)
		}
	}

	if len(selected) > 0 {
		return nil, fmt.Errorf("unknown provider %q (available: %s)", firstSelectedProvider(selected), strings.Join(availableProviderNames(), ", "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return out, nil
}

func selectedProviderNames(providerNames []string) map[string]struct{} {
	selected := map[string]struct{}{}
	for _, providerName := range providerNames {
		providerName = strings.ToLower(strings.TrimSpace(providerName))
		if providerName == "" {
			continue
		}
		selected[providerName] = struct{}{}
	}
	return selected
}

func firstSelectedProvider(selected map[string]struct{}) string {
	for providerName := range selected {
		return providerName
	}
	return ""
}

func availableProviderNames() []string {
	names := make([]string, 0, len(providerFactories))
	for _, factory := range providerFactories {
		names = append(names, factory.name)
	}
	return names
}
