package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/cryptoutil"
	"mvdl/internal/domain"
	"mvdl/internal/knaben"
	"mvdl/internal/metadata"
	"mvdl/internal/provider"
	"mvdl/internal/search"
	"mvdl/internal/server"
	"mvdl/internal/torrentclaw"
)

func runServer(c *cli.Context) error {
	cfg, err := newServerConfig(c)
	if err != nil {
		return err
	}

	aggregator := provider.NewAggregator([]provider.Provider{
		knaben.NewClient(KNABEN_API_URL, cfg.HTTPClient),
		torrentclaw.NewClient(TORRENTCLAW_API_URL, cfg.HTTPClient),
	}...)
	handler := server.NewHandler(
		search.NewService(newMetadataResolver(cfg.HTTPClient), aggregator),
		cfg,
	)

	logrus.WithFields(logrus.Fields{
		"listen":   cfg.Addr,
		"pageSize": cfg.PageSize,
		"timeout":  cfg.HTTPClient.Timeout.String(),
	}).Info("server listening")

	if err := http.ListenAndServe(cfg.Addr, handler.Routes()); err != nil {
		return fmt.Errorf("server stopped: %w", err)
	}
	return nil
}

func newServerConfig(c *cli.Context) (server.Config, error) {
	magnetEncryptor, err := newMagnetEncryptor()
	if err != nil {
		return server.Config{}, err
	}

	return server.Config{
		Addr:     c.String(FlagListen),
		PageSize: c.Int(FlagPageSize),
		HTTPClient: &http.Client{
			Timeout: time.Duration(c.Int(FlagTimeout)) * time.Second,
		},
		MagnetEncryptor: magnetEncryptor,
	}, nil
}

func newMagnetEncryptor() (domain.StringEncryptor, error) {
	key := envString(MVDL_CRYKEY, "")
	if key == "" {
		logrus.Warnf("magnetUrl encryption disabled: environment var %s is not set", MVDL_CRYKEY)
		return nil, nil
	}

	encryptor, err := cryptoutil.NewStringEncryptor(key)
	if err != nil {
		return nil, fmt.Errorf("invalid magnetUrl encryption key: %w", err)
	}
	return encryptor, nil
}

func newMetadataResolver(client *http.Client) metadata.Resolver {
	apiKey := envString("MVDL_TMDB_APIKEY", "")
	if apiKey == "" {
		logrus.Warnf("tmdb resolver disabled: MVDL_TMDB_APIKEY is not set")
		return nil
	}

	return metadata.NewTMDBClient(metadata.TMDBOptions{
		APIURL:     "https://api.themoviedb.org/3",
		APIKey:     apiKey,
		HTTPClient: client,
	})
}

func envString(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
