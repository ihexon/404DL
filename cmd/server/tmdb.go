package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/urfave/cli/v2"

	"mvdl/internal/metadata"
)

func runTMDB(c *cli.Context) error {
	query := c.Args().First()
	if query == "" {
		return fmt.Errorf("search term is required")
	}

	mediaType, err := tmdbMediaType(c)
	if err != nil {
		return err
	}

	apiKey := envString(envTMDBAPIKey, "")
	if apiKey == "" {
		return fmt.Errorf("%s is required", envTMDBAPIKey)
	}

	client := metadata.NewTMDBClient(metadata.TMDBOptions{
		APIURL:   envString(envTMDBAPIURL, defaultTMDBAPIURL),
		APIKey:   apiKey,
		Language: c.String(FlagLanguage),
		HTTPClient: &http.Client{
			Timeout: c.Duration(FlagTimeout),
		},
	})

	results, err := client.SearchAggregated(c.Context, mediaType, query)
	if err != nil {
		return fmt.Errorf("query tmdb: %w", err)
	}
	if len(results.Results) == 0 {
		return fmt.Errorf("tmdb returned no %s results for %q", mediaType, query)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(metadata.FormatTMDBSearchResponse(results))
}

func tmdbMediaType(c *cli.Context) (metadata.TMDBMediaType, error) {
	isMovie := c.Bool(FlagMovie)
	isTV := c.Bool(FlagTV)
	if isMovie == isTV {
		return "", fmt.Errorf("exactly one of --%s or --%s is required", FlagMovie, FlagTV)
	}
	if isMovie {
		return metadata.TMDBMediaMovie, nil
	}
	return metadata.TMDBMediaTV, nil
}
