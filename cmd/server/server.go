package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"4dl/internal/crypto"
	"4dl/internal/responsecodec"
	"4dl/internal/server"
)

func runServer(c *cli.Context) error {
	cfg, err := newServerConfig(c)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"listen":                        cfg.Addr,
		"default_limit":                 cfg.DefaultLimit,
		"upstream_timeout":              cfg.HTTPClient.Timeout.String(),
		"response_encryption_available": cfg.ResponseEncryptor != nil,
	}).Info("server configured")

	handler := newSearchHandler(cfg)

	logrus.WithFields(logrus.Fields{
		"listen": cfg.Addr,
	}).Info("server listening")

	if err := http.ListenAndServe(cfg.Addr, handler.Routes()); err != nil {
		return fmt.Errorf("server stopped: %w", err)
	}
	return nil
}

func newSearchHandler(cfg server.Config) *server.Handler {
	searcher := newSearchAggregator(cfg.HTTPClient)
	return server.NewHandler(searcher, cfg)
}

func newServerConfig(c *cli.Context) (server.Config, error) {
	cfg, err := newSearchServerConfig(c.Int(FlagLimitSize), c.Duration(FlagTimeout), true)
	if err != nil {
		return server.Config{}, err
	}
	cfg.Addr = c.String(FlagListen)
	return cfg, nil
}

func newSearchServerConfig(defaultLimit int, upstreamTimeout time.Duration, warnMissingResponseKey bool) (server.Config, error) {
	responseEncryptor, err := newResponseEncryptor(warnMissingResponseKey)
	if err != nil {
		return server.Config{}, err
	}
	return server.Config{
		DefaultLimit: server.NormalizeLimit(defaultLimit),
		HTTPClient: &http.Client{
			Timeout: upstreamTimeout,
		},
		ResponseEncryptor: responseEncryptor,
	}, nil
}

func newResponseEncryptor(warnMissing bool) (responsecodec.Encryptor, error) {
	encryptor, enabled, err := newOptionalResponseEncryptor()
	if !enabled {
		if warnMissing {
			logrus.Warnf("server response encryption disabled: environment var %s is not set", envCryptoKey)
		}
		return nil, nil
	}
	return encryptor, err
}

func newOptionalResponseEncryptor() (responsecodec.Encryptor, bool, error) {
	key := secretEnvString("", envCryptoKey)
	if key == "" {
		return nil, false, nil
	}
	encryptor, err := crypto.NewStringEncryptor(key)
	if err != nil {
		return nil, true, fmt.Errorf("invalid response encryption key: %w", err)
	}
	logrus.WithField("key_length", len(key)).Info("server response encryption enabled")
	return encryptor, true, nil
}

func secretEnvString(fallback string, names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return fallback
}
