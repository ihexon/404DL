package main

import (
	"time"

	"github.com/urfave/cli/v2"

	searchapi "mvdl/internal/server"
)

func newApp() *cli.App {
	return &cli.App{
		Name:  "mvdl",
		Usage: "file search and download utility",
		Commands: []*cli.Command{
			newServerCommand(),
			newSearchCommand(),
			newGetCommand(),
		},
	}
}

func newServerCommand() *cli.Command {
	return &cli.Command{
		Name:   SubCmdServer,
		Usage:  "start file search API server",
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
			Name:    FlagLimitSize,
			Usage:   "maximum returned results, default 50",
			Value:   searchapi.DefaultSearchLimit,
			EnvVars: []string{envLimitSize},
		},
		&cli.DurationFlag{
			Name:    FlagTimeout,
			Usage:   "upstream timeout, default 8s",
			Value:   8 * time.Second,
			EnvVars: []string{envUpstreamTimeout},
		},
	}
}

func newSearchCommand() *cli.Command {
	return &cli.Command{
		Name:      SubCmdSearch,
		Usage:     "search files through providers",
		UsageText: "mvdl search <query>",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:  FlagTimeout,
				Usage: "search timeout, default 8s",
				Value: 8 * time.Second,
			},
			&cli.IntFlag{
				Name:  FlagLimitSize,
				Usage: "maximum returned results, default 50",
				Value: searchapi.DefaultSearchLimit,
			},
			&cli.StringSliceFlag{
				Name:  FlagProvider,
				Usage: "debug selected providers; repeat for multiple providers, for example --provider knaben --provider torrentclaw",
			},
		},
		Action: runSearch,
	}
}

func newGetCommand() *cli.Command {
	return &cli.Command{
		Name:      SubCmdGet,
		Usage:     "download selected search results through BitTorrent",
		UsageText: "mvdl search <query> | mvdl get --stdin\n   mvdl get --input results.json",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  FlagInput,
				Usage: "search result JSON input file",
			},
			&cli.BoolFlag{
				Name:  FlagStdin,
				Usage: "read search result JSON from stdin",
			},
			&cli.StringFlag{
				Name:  FlagListen,
				Usage: "HTTP listen address",
				Value: DefaultGetAddr,
			},
			&cli.StringFlag{
				Name:  FlagSaveTo,
				Usage: "directory to save downloaded files",
			},
			&cli.StringFlag{
				Name:  FlagTorrentListen,
				Usage: "torrent listen address",
				Value: DefaultTorrentListenAddr,
			},
		},
		Action: runGet,
	}
}
