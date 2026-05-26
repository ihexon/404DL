package main

import (
	"fmt"
	"io"
	"mvdl/internal/domain"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/cryptoutil"
	"mvdl/internal/knaben"
	"mvdl/internal/metadata"
	"mvdl/internal/provider"
	"mvdl/internal/search"
	"mvdl/internal/server"
	"mvdl/internal/torrentclaw"
)

const (
	MVDL_CRYKEY = "MVDL_CRYKEY"

	FlagPageSize   = "page-size"
	FlagListen     = "listen"
	FlagResolution = "resolution"
	FlagAddr       = "addr"
	FlagTimeout    = "timeout"

	DefaultListenAddr = "127.0.0.1:6567"

	SubCmdQuery  = "query"
	SubCmdServer = "server"

	KNABEN_API_URL      = "https://api.knaben.org/v1"
	TORRENTCLAW_API_URL = "https://torrentclaw.com/api/v1"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	app := &cli.App{
		Name:  "mvdl",
		Usage: "movie torrent search utility",
		Commands: []*cli.Command{
			{
				Name:  SubCmdServer,
				Usage: "start movie search API server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  FlagListen,
						Usage: "listen address",
						Value: DefaultListenAddr,
					},
					&cli.IntFlag{
						Name:  FlagPageSize,
						Usage: "return page size, default 50",
						Value: 50,
					},
					&cli.IntFlag{
						Name:  FlagTimeout,
						Usage: "upstream timeout, default 8s",
						Value: 8,
					},
				},
				Action: runServer,
			},
			{
				Name:      SubCmdQuery,
				Usage:     "query a running movie search API server",
				UsageText: "mvdl query <movie name> --resolution 1080p [--addr http://127.0.0.1:8080]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     FlagResolution,
						Usage:    "video resolution, for example 1080p or 2160p",
						Required: true,
					},
					&cli.StringFlag{
						Name:  FlagAddr,
						Usage: "movie search API base URL",
						Value: "http://" + DefaultListenAddr,
					},
					&cli.IntFlag{
						Name:  FlagTimeout,
						Usage: "search timeout, default 8s",
						Value: 8,
					},
				},
				Action: runSearch,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		logrus.WithError(err).Fatal("command failed")
	}
}

func runServer(c *cli.Context) error {
	var magnetEncryptor domain.StringEncryptor

	if key := envString(MVDL_CRYKEY, ""); key != "" {
		encryptor, err := cryptoutil.NewStringEncryptor(key)
		if err != nil {
			return fmt.Errorf("invalid magnetUrl encryption key: %w", err)
		}

		magnetEncryptor = encryptor
	} else {
		logrus.Warnf("magnetUrl encryption disabled: environment var %s is not set", MVDL_CRYKEY)
		magnetEncryptor = nil
	}

	cfg := server.Config{
		Addr:     c.String(FlagListen),
		PageSize: c.Int(FlagPageSize),
		HTTPClient: &http.Client{
			Timeout: time.Duration(c.Int(FlagTimeout)) * time.Second,
		},
		MagnetEncryptor: magnetEncryptor,
	}

	aggregator := provider.NewAggregator([]provider.Provider{
		knaben.NewClient(KNABEN_API_URL, cfg.HTTPClient),
		torrentclaw.NewClient(TORRENTCLAW_API_URL, cfg.HTTPClient),
	}...)

	var resolver metadata.Resolver

	// if user set MVDL_TMDB_APIKEY, using tmdb resolver
	if apiKey := envString("MVDL_TMDB_APIKEY", ""); apiKey != "" {
		resolver = metadata.NewTMDBClient(metadata.TMDBOptions{
			APIURL:     "https://api.themoviedb.org/3",
			APIKey:     apiKey,
			HTTPClient: cfg.HTTPClient,
		})
	} else {
		logrus.Warnf("tmdb resolver disabled: MVDL_TMDB_APIKEY is not set")
	}

	handler := server.NewHandler(search.NewService(resolver, aggregator), cfg)

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

func runSearch(c *cli.Context) error {
	searchName := c.Args().First()
	if searchName == "" {
		return fmt.Errorf("movie name is required")
	}

	baseURL, err := url.Parse(c.String("addr"))
	if err != nil {
		return fmt.Errorf("parse addr: %w", err)
	}
	baseURL.Path = "/v1/t"
	query := baseURL.Query()
	query.Set("search", searchName)
	query.Set("resolution", c.String("resolution"))
	baseURL.RawQuery = query.Encode()

	client := &http.Client{
		Timeout: time.Duration(c.Int(FlagTimeout)) * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mvdl/1.0")

	logrus.WithFields(logrus.Fields{
		"addr":       c.String("addr"),
		"search":     searchName,
		"resolution": c.String("resolution"),
	}).Info("search request started")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call search API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("search API returned %d: %s", resp.StatusCode, string(body))
	}

	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("write search response: %w", err)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func envString(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
