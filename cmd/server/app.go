package main

import (
	"time"

	"github.com/urfave/cli/v2"
)

func newApp() *cli.App {
	return &cli.App{
		Name:  "mvdl",
		Usage: "movie torrent search utility",
		Action: func(*cli.Context) error {
			return cli.Exit("missing command", 1)
		},
		Commands: []*cli.Command{
			newServerCommand(),
			newQueryCommand(),
			newDownloadCommand(),
		},
	}
}

func newServerCommand() *cli.Command {
	return &cli.Command{
		Name:   SubCmdServer,
		Usage:  "start movie search API server",
		Flags:  serverFlags(),
		Action: runServer,
	}
}

func serverFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    FlagListen,
			Usage:   "listen address",
			Value:   DefaultListenAddr,
			EnvVars: []string{envAddr},
		},
		&cli.IntFlag{
			Name:    FlagPageSize,
			Usage:   "return page size, default 50",
			Value:   50,
			EnvVars: []string{envPageSize},
		},
		&cli.DurationFlag{
			Name:    FlagTimeout,
			Usage:   "upstream timeout, default 8s",
			Value:   8 * time.Second,
			EnvVars: []string{envUpstreamTimeout},
		},
	}
}

func newQueryCommand() *cli.Command {
	return &cli.Command{
		Name:      SubCmdQuery,
		Usage:     "query torrent providers directly",
		UsageText: "mvdl query [--filter keyword] <search term>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagFilter,
				Usage: "optional case-insensitive result filter, for example 1080p, 2160p, or linux",
			},
			&cli.DurationFlag{
				Name:  FlagTimeout,
				Usage: "search timeout, default 8s",
				Value: 8 * time.Second,
			},
			&cli.IntFlag{
				Name:  FlagPageSize,
				Usage: "return page size, default 50",
				Value: 50,
			},
			&cli.StringSliceFlag{
				Name:  FlagProvider,
				Usage: "debug selected providers; repeat for multiple providers, for example --provider knaben --provider torrentclaw",
			},
		},
		Action: runSearch,
	}
}

func newDownloadCommand() *cli.Command {
	return &cli.Command{
		Name:      SubCmdDownload,
		Usage:     "download a magnet URL or .torrent file",
		UsageText: "mvdl download --save-to ./downloads <magnet-url|encrypted-magnet-url|torrent-file>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagSaveTo,
				Usage:    "directory to save downloaded files",
				Required: true,
			},
			&cli.IntFlag{
				Name:  FlagConnections,
				Usage: "maximum established peer connections per torrent",
				Value: 160,
			},
			&cli.IntFlag{
				Name:  FlagHalfOpen,
				Usage: "maximum dialing peer connections per torrent",
				Value: 80,
			},
			&cli.IntFlag{
				Name:  FlagTotalHalfOpen,
				Usage: "maximum dialing peer connections across the client",
				Value: 240,
			},
			&cli.IntFlag{
				Name:  FlagPeerHighWater,
				Usage: "maximum peer addresses kept for the torrent",
				Value: 2000,
			},
			&cli.IntFlag{
				Name:  FlagPeerLowWater,
				Usage: "peer address count below which more peers are requested",
				Value: 200,
			},
			&cli.IntFlag{
				Name:  FlagDialRate,
				Usage: "peer dial attempts per second",
				Value: 80,
			},
			&cli.IntFlag{
				Name:  FlagMaxUnverifiedMiB,
				Usage: "maximum downloaded MiB pending piece verification",
				Value: 512,
			},
			&cli.IntFlag{
				Name:  FlagPeerRequestBufferMiB,
				Usage: "maximum MiB buffered per peer for upload request data",
				Value: 4,
			},
			&cli.IntFlag{
				Name:  FlagPieceHashers,
				Usage: "piece hash verification workers per torrent",
				Value: 4,
			},
			&cli.StringFlag{
				Name:  FlagTorrentListen,
				Usage: "torrent listen address, for example :42069 or 0.0.0.0:0",
			},
			&cli.BoolFlag{
				Name:  FlagNoUpload,
				Usage: "disable all peer uploads; this can reduce download speed in many swarms",
			},
			&cli.BoolFlag{
				Name:  FlagSeed,
				Usage: "continue altruistic upload while downloading and after completion",
			},
			&cli.BoolFlag{
				Name:  FlagDisableIPv6,
				Usage: "disable IPv6 torrent networking",
			},
			&cli.IntFlag{
				Name:  FlagDownloadRateMiB,
				Usage: "download rate limit in MiB/s, 0 means unlimited",
			},
			&cli.IntFlag{
				Name:  FlagUploadRateMiB,
				Usage: "upload rate limit in MiB/s, 0 means unlimited",
			},
			&cli.IntFlag{
				Name:  FlagProgressInterval,
				Usage: "download progress report interval in seconds",
				Value: 1,
			},
			&cli.StringFlag{
				Name:  FlagStatusListen,
				Usage: "optional download status HTTP listen address, for example 127.0.0.1:6570",
			},
		},
		Action: runDownload,
	}
}
