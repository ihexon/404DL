package main

import (
	"net/http"

	"github.com/sirupsen/logrus"

	"4dl/internal/knaben"
	"4dl/internal/provider"
	"4dl/internal/torrentclaw"
)

type providerFactory struct {
	new func(*http.Client) provider.Provider
}

var providerFactories = []providerFactory{
	{
		new: func(client *http.Client) provider.Provider {
			return knaben.NewClient(defaultKnabenAPIURL, client)
		},
	},
	{
		new: func(client *http.Client) provider.Provider {
			return torrentclaw.NewClient(
				defaultTorrentClawURL,
				client,
				torrentclaw.WithAPIKey(secretEnvString("", envTorrentClawAPIKey)),
			)
		},
	},
}

func newSearchAggregator(client *http.Client) *provider.Aggregator {
	providers := newSearchProviders(client)
	logrus.WithFields(logrus.Fields{
		"providers": providerNamesFromInstances(providers),
		"timeout":   clientTimeoutString(client),
	}).Info("search aggregator configured")
	return provider.NewAggregator(providers...)
}

func newSearchProviders(client *http.Client) []provider.Provider {
	out := make([]provider.Provider, 0, len(providerFactories))
	for _, factory := range providerFactories {
		out = append(out, factory.new(client))
	}
	return out
}

func providerNamesFromInstances(providers []provider.Provider) []string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name())
	}
	return names
}

func clientTimeoutString(client *http.Client) string {
	if client == nil {
		return ""
	}
	return client.Timeout.String()
}
