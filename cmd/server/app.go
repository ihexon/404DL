package main

import (
	"time"

	"github.com/urfave/cli/v2"
)

func newApp() *cli.App {
	return &cli.App{
		Name:   "4dl",
		Usage:  "start the 404 Downloader web UI",
		Flags:  appFlags(),
		Action: runWeb,
	}
}

func appFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  FlagListen,
			Usage: "HTTP listen address",
			Value: DefaultGetAddr,
		},
		&cli.IntFlag{
			Name:  FlagLimitSize,
			Usage: "maximum search results, default 50",
			Value: DefaultSearchLimit,
		},
		&cli.DurationFlag{
			Name:  FlagTimeout,
			Usage: "upstream timeout, default 8s",
			Value: 8 * time.Second,
		},
		&cli.StringFlag{
			Name:  FlagDownloadDir,
			Usage: "default directory to save downloaded files",
		},
		&cli.StringFlag{
			Name:  FlagStateDir,
			Usage: "directory for application state and torrent resume metadata",
		},
		&cli.StringFlag{
			Name:  FlagTorrentListen,
			Usage: "torrent listen address",
			Value: DefaultTorrentListenAddr,
		},
	}
}
