package main

import (
	"net/http"

	"mvdl/internal/domain"
	"mvdl/internal/knaben"
	"mvdl/internal/provider"
	"mvdl/internal/search"
	"mvdl/internal/torrentclaw"
)

func newTorrentSearcher(client *http.Client) domain.TorrentSearcher {
	aggregator := provider.NewAggregator([]provider.Provider{
		knaben.NewClient(KNABEN_API_URL, client),
		torrentclaw.NewClient(TORRENTCLAW_API_URL, client),
	}...)

	return search.NewService(newMetadataResolver(client), aggregator)
}
