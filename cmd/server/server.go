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
	"mvdl/internal/metadata"
	"mvdl/internal/server"
)

func runServer(c *cli.Context) error {
	cfg, err := newServerConfig(c)
	if err != nil {
		return err
	}

	handler, err := newSearchHandler(cfg)
	if err != nil {
		return err
	}

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

func newSearchHandler(cfg server.Config, providerNames ...string) (*server.Handler, error) {
	searcher, err := newTorrentSearcher(cfg.HTTPClient, providerNames...)
	if err != nil {
		return nil, err
	}
	return server.NewHandler(searcher, cfg), nil
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
