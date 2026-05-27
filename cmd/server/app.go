package main

import (
	"time"

	"github.com/urfave/cli/v2"
)

func newApp() *cli.App {
	return &cli.App{
		Name:  "mvdl",
		Usage: "movie torrent search utility",
		Commands: []*cli.Command{
			newServerCommand(),
			newQueryCommand(),
			newHTTPFSCommand(),
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
		UsageText: "mvdl query <search term>",
		Flags: []cli.Flag{
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

func newHTTPFSCommand() *cli.Command {
	return &cli.Command{
		Name:      SubCmdHTTPFS,
		Usage:     "serve query results as a local HTTP file index",
		UsageText: "mvdl query <search term> | mvdl httpfs --stdin\n   mvdl httpfs --input results.json",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagInput,
				Usage: "query JSON input file",
			},
			&cli.BoolFlag{
				Name:  FlagStdin,
				Usage: "read query JSON from stdin",
			},
			&cli.StringFlag{
				Name:  FlagListen,
				Usage: "HTTP listen address",
				Value: DefaultHTTPFSAddr,
			},
			&cli.StringFlag{
				Name:  FlagDataDir,
				Usage: "directory for torrent data and metadata cache",
			},
			&cli.StringFlag{
				Name:  FlagTorrentListen,
				Usage: "torrent listen address",
				Value: DefaultTorrentListenAddr,
			},
		},
		Action: runHTTPFS,
	}
}
