package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/crypto"
	"mvdl/internal/server"
)

func runServer(c *cli.Context) error {
	cfg, err := newServerConfig(c)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"listen":            cfg.Addr,
		"page_size":         cfg.PageSize,
		"upstream_timeout":  cfg.HTTPClient.Timeout.String(),
		"magnet_encryption": cfg.MagnetEncryptor != nil,
	}).Info("server configured")

	handler, err := newSearchHandler(cfg)
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"listen": cfg.Addr,
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
			Timeout: c.Duration(FlagTimeout),
		},
		MagnetEncryptor: magnetEncryptor,
	}, nil
}

func newMagnetEncryptor() (server.StringEncryptor, error) {
	encryptor, enabled, err := newOptionalMagnetEncryptor()
	if !enabled {
		logrus.Warnf("magnetUrl encryption disabled: environment var %s is not set", envCryptoKey)
		return nil, nil
	}
	return encryptor, err
}

func newOptionalMagnetEncryptor() (server.StringEncryptor, bool, error) {
	key := envString(envCryptoKey, "")
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

func envString(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
