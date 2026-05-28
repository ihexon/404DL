package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/logging"
	"mvdl/internal/provider"
	"mvdl/internal/server"
)

func runSearch(c *cli.Context) error {
	startedAt := time.Now()
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

	requestID := logging.NewRequestID()
	logrus.WithFields(logrus.Fields{
		"request_id": requestID,
		"search":     searchName,
		"providers":  providerNames,
		"limit":      c.Int(FlagPageSize),
		"timeout":    client.Timeout.String(),
	}).Info("search request started")

	ctx := logging.WithRequestID(c.Context, requestID)
	hits, err := searcher.Search(ctx, provider.SearchRequest{
		Query: searchName,
		Limit: c.Int(FlagPageSize),
	})
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"request_id":  requestID,
			"search":      searchName,
			"providers":   providerNames,
			"duration_ms": logging.DurationMillis(time.Since(startedAt)),
		}).Error("search request failed")
		return fmt.Errorf("search torrents: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"request_id":  requestID,
		"search":      searchName,
		"providers":   providerNames,
		"count":       len(hits),
		"duration_ms": logging.DurationMillis(time.Since(startedAt)),
	}).Info("search request completed")

	encryptor, encryptMagnets, err := newOptionalMagnetEncryptor()
	if err != nil {
		return err
	}
	if encryptMagnets {
		encrypted, encryptedCount, err := server.EncryptMagnets(hits, encryptor)
		if err != nil {
			return fmt.Errorf("encrypt magnetUrl: %w", err)
		}
		hits = encrypted
		logrus.WithFields(logrus.Fields{
			"request_id":        requestID,
			"encrypted_magnets": encryptedCount,
			"magnet_encryption": true,
		}).Info("query magnet URLs encrypted")
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(hits)
}
