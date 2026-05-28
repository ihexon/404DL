package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"4dl/internal/crypto"
	"4dl/internal/server"
)

func runServer(c *cli.Context) error {
	cfg, err := newServerConfig(c)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"listen":            cfg.Addr,
		"default_limit":     cfg.DefaultLimit,
		"upstream_timeout":  cfg.HTTPClient.Timeout.String(),
		"magnet_encryption": cfg.MagnetEncryptor != nil,
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

func newSearchServerConfig(defaultLimit int, upstreamTimeout time.Duration, warnMissingCryptoKey bool) (server.Config, error) {
	magnetEncryptor, err := newMagnetEncryptor(warnMissingCryptoKey)
	if err != nil {
		return server.Config{}, err
	}
	return server.Config{
		DefaultLimit: server.NormalizeLimit(defaultLimit),
		HTTPClient: &http.Client{
			Timeout: upstreamTimeout,
		},
		MagnetEncryptor: magnetEncryptor,
	}, nil
}

func newMagnetEncryptor(warnMissing bool) (server.StringEncryptor, error) {
	encryptor, enabled, err := newOptionalMagnetEncryptor()
	if !enabled {
		if warnMissing {
			logrus.Warnf("magnetUrl encryption disabled: environment var %s is not set", envCryptoKey)
		}
		return nil, nil
	}
	return encryptor, err
}

func newOptionalMagnetEncryptor() (server.StringEncryptor, bool, error) {
	key := secretEnvString("", envCryptoKey)
	if key == "" {
		return nil, false, nil
	}
	encryptor, err := crypto.NewStringEncryptor(key)
	if err != nil {
		return nil, true, fmt.Errorf("invalid magnetUrl encryption key: %w", err)
	}
	logrus.WithField("key_length", len(key)).Info("magnetUrl encryption enabled")
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
