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
	searchQuery := c.Args().First()
	if searchQuery == "" {
		return fmt.Errorf("query is required")
	}

	client := &http.Client{Timeout: c.Duration(FlagTimeout)}

	providerNames := c.StringSlice(FlagProvider)
	searcher, err := newSearchAggregator(client, providerNames...)
	if err != nil {
		return err
	}

	requestID := logging.NewRequestID()
	logrus.WithFields(logrus.Fields{
		"request_id": requestID,
		"query":      searchQuery,
		"providers":  providerNames,
		"limit":      c.Int(FlagLimitSize),
		"timeout":    client.Timeout.String(),
	}).Info("search request started")

	ctx := logging.WithRequestID(c.Context, requestID)
	results, err := searcher.Search(ctx, provider.SearchRequest{
		Query: searchQuery,
		Limit: c.Int(FlagLimitSize),
	})
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"request_id":  requestID,
			"query":       searchQuery,
			"providers":   providerNames,
			"duration_ms": logging.DurationMillis(time.Since(startedAt)),
		}).Error("search request failed")
		return fmt.Errorf("search: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"request_id":  requestID,
		"query":       searchQuery,
		"providers":   providerNames,
		"count":       len(results),
		"duration_ms": logging.DurationMillis(time.Since(startedAt)),
	}).Info("search request completed")

	encryptor, encryptMagnets, err := newOptionalMagnetEncryptor()
	if err != nil {
		return err
	}
	if encryptMagnets {
		encrypted, encryptedCount, err := server.EncryptMagnetURLs(results, encryptor)
		if err != nil {
			return fmt.Errorf("encrypt magnetUrl: %w", err)
		}
		results = encrypted
		logrus.WithFields(logrus.Fields{
			"request_id":        requestID,
			"encrypted_magnets": encryptedCount,
			"magnet_encryption": true,
		}).Info("search result magnet URLs encrypted")
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}
