package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/httpfs"
)

func runHTTPFS(c *cli.Context) error {
	inputPath, ok, err := httpfsInputPath(c)
	if err != nil || !ok {
		return err
	}

	dataDir, err := httpfsDataDir(c.String(FlagDataDir))
	if err != nil {
		return err
	}

	cfg := httpfs.Config{
		ListenAddr:        c.String(FlagListen),
		InputPath:         inputPath,
		DataDir:           dataDir,
		TorrentListenAddr: c.String(FlagTorrentListen),
		CryptoKey:         envString(envCryptoKey, ""),
	}

	logrus.WithFields(logrus.Fields{
		"listen": cfg.ListenAddr,
		"input":  cfg.InputPath,
		"data":   cfg.DataDir,
	}).Info("httpfs starting")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return httpfs.Run(ctx, cfg)
}

func httpfsInputPath(c *cli.Context) (string, bool, error) {
	hasInput := c.IsSet(FlagInput)
	hasStdin := c.Bool(FlagStdin)
	if hasInput && hasStdin {
		return "", false, fmt.Errorf("--%s and --%s cannot be used together", FlagInput, FlagStdin)
	}
	if hasInput {
		return c.String(FlagInput), true, nil
	}
	if hasStdin {
		return "-", true, nil
	}
	if err := cli.ShowSubcommandHelp(c); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func httpfsDataDir(value string) (string, error) {
	if value != "" {
		return value, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(cacheDir, "mvdl", "httpfs"), nil
}
