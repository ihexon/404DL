package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/provider"
)

func runSearch(c *cli.Context) error {
	searchName := c.Args().First()
	if searchName == "" {
		return fmt.Errorf("movie name is required")
	}

	client := &http.Client{Timeout: c.Duration(FlagTimeout)}

	providerNames := c.StringSlice(FlagProvider)
	searcher, err := newTorrentSearcher(client, providerNames...)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"search":    searchName,
		"providers": providerNames,
	}).Info("search request started")

	hits, err := searcher.Search(c.Context, provider.SearchRequest{
		Query: searchName,
		Limit: c.Int(FlagPageSize),
	})
	if err != nil {
		return fmt.Errorf("search torrents: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(hits)
}
