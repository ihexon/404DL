package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	downloadui "4dl/internal/get"
)

func runWeb(c *cli.Context) error {
	upstreamClient := &http.Client{Timeout: c.Duration(FlagTimeout)}
	cfg := downloadui.Config{
		ListenAddr:        c.String(FlagListen),
		DownloadDir:       c.String(FlagDownloadDir),
		StateDir:          c.String(FlagStateDir),
		TorrentListenAddr: c.String(FlagTorrentListen),
		Searcher:          newSearchAggregator(upstreamClient),
		DefaultLimit:      c.Int(FlagLimitSize),
	}

	logrus.WithFields(logrus.Fields{
		"listen":           cfg.ListenAddr,
		"download_dir":     cfg.DownloadDir,
		"state_dir":        cfg.StateDir,
		"upstream_timeout": upstreamClient.Timeout.String(),
		"default_limit":    cfg.DefaultLimit,
	}).Info("web UI starting")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return downloadui.Run(ctx, cfg)
}
