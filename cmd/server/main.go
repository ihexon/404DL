package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/cryptoutil"
	"mvdl/internal/domain"
	"mvdl/internal/downloader"
	"mvdl/internal/knaben"
	"mvdl/internal/metadata"
	"mvdl/internal/provider"
	"mvdl/internal/search"
	"mvdl/internal/server"
	"mvdl/internal/torrentclaw"
)

const (
	MVDL_CRYKEY = "MVDL_CRYKEY"

	FlagPageSize             = "page-size"
	FlagListen               = "listen"
	FlagResolution           = "resolution"
	FlagAddr                 = "addr"
	FlagTimeout              = "timeout"
	FlagSaveTo               = "save-to"
	FlagConnections          = "connections"
	FlagHalfOpen             = "half-open"
	FlagTotalHalfOpen        = "total-half-open"
	FlagPeerHighWater        = "peer-high-water"
	FlagPeerLowWater         = "peer-low-water"
	FlagDialRate             = "dial-rate"
	FlagMaxUnverifiedMiB     = "max-unverified-mib"
	FlagPeerRequestBufferMiB = "peer-request-buffer-mib"
	FlagPieceHashers         = "piece-hashers"
	FlagTorrentListen        = "torrent-listen"
	FlagNoUpload             = "no-upload"
	FlagSeed                 = "seed"
	FlagDisableIPv6          = "disable-ipv6"
	FlagDownloadRateMiB      = "download-rate-mib"
	FlagUploadRateMiB        = "upload-rate-mib"
	FlagProgressInterval     = "progress-interval"
	FlagStatusListen         = "status-listen"

	DefaultListenAddr = "127.0.0.1:6567"

	SubCmdDownload = "download"
	SubCmdQuery    = "query"
	SubCmdServer   = "server"

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
			{
				Name:      SubCmdDownload,
				Usage:     "download a magnet URL or .torrent file",
				UsageText: "mvdl download <magnet-url|encrypted-magnet-url|torrent-file> --save-to ./downloads",
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

func runDownload(c *cli.Context) error {
	value := c.Args().First()
	if value == "" {
		return fmt.Errorf("magnet URL or torrent file is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	downloadCfg := downloader.Config{
		DataDir:                    c.String(FlagSaveTo),
		ProgressWriter:             os.Stdout,
		ProgressInterval:           time.Duration(c.Int(FlagProgressInterval)) * time.Second,
		StatusAddr:                 c.String(FlagStatusListen),
		EstablishedConnsPerTorrent: c.Int(FlagConnections),
		HalfOpenConnsPerTorrent:    c.Int(FlagHalfOpen),
		TotalHalfOpenConns:         c.Int(FlagTotalHalfOpen),
		TorrentPeersHighWater:      c.Int(FlagPeerHighWater),
		TorrentPeersLowWater:       c.Int(FlagPeerLowWater),
		MaxUnverifiedBytes:         int64(c.Int(FlagMaxUnverifiedMiB)) << 20,
		MaxPeerRequestBufferBytes:  c.Int(FlagPeerRequestBufferMiB) << 20,
		DialRateLimit:              float64(c.Int(FlagDialRate)),
		DialRateBurst:              c.Int(FlagDialRate),
		PieceHashersPerTorrent:     c.Int(FlagPieceHashers),
		ListenAddr:                 c.String(FlagTorrentListen),
		NoUpload:                   c.Bool(FlagNoUpload),
		Seed:                       c.Bool(FlagSeed),
		DisableIPv6:                c.Bool(FlagDisableIPv6),
		DownloadRateBytesPerSec:    c.Int(FlagDownloadRateMiB) << 20,
		UploadRateBytesPerSec:      c.Int(FlagUploadRateMiB) << 20,
	}

	downloadKind := "magnet"
	download := func() error {
		magnetURL, err := resolveMagnetURL(value)
		if err != nil {
			return err
		}
		return downloader.DownloadMagnet(ctx, magnetURL, downloadCfg)
	}
	if isTorrentFile(value) {
		downloadKind = "torrent"
		download = func() error {
			return downloader.DownloadTorrentFile(ctx, value, downloadCfg)
		}
	}

	logrus.WithFields(logrus.Fields{
		"saveTo":      downloadCfg.DataDir,
		"type":        downloadKind,
		"status":      downloadCfg.StatusAddr,
		"connections": downloadCfg.EstablishedConnsPerTorrent,
		"halfOpen":    downloadCfg.HalfOpenConnsPerTorrent,
		"dialRate":    downloadCfg.DialRateLimit,
	}).Info("download started")
	if err := download(); err != nil {
		if errors.Is(err, context.Canceled) {
			logrus.WithField("saveTo", c.String(FlagSaveTo)).Info("download interrupted")
			return nil
		}
		return err
	}
	logrus.WithField("saveTo", c.String(FlagSaveTo)).Info("download completed")
	return nil
}

func resolveMagnetURL(value string) (string, error) {
	if isMagnetURL(value) {
		return value, nil
	}

	key := envString(MVDL_CRYKEY, "")
	if key == "" {
		return "", fmt.Errorf("%s is required to decrypt encrypted magnet URL", MVDL_CRYKEY)
	}

	decryptor, err := cryptoutil.NewStringEncryptor(key)
	if err != nil {
		return "", fmt.Errorf("invalid magnetUrl encryption key: %w", err)
	}

	magnetURL, err := decryptor.DecryptString(value)
	if err != nil {
		return "", fmt.Errorf("decrypt magnetUrl: %w", err)
	}
	if !isMagnetURL(magnetURL) {
		return "", fmt.Errorf("decrypted value is not a magnet URL")
	}
	return magnetURL, nil
}

func isMagnetURL(value string) bool {
	return strings.HasPrefix(value, "magnet:")
}

func isTorrentFile(value string) bool {
	info, err := os.Stat(value)
	return err == nil && !info.IsDir() && strings.EqualFold(filepath.Ext(value), ".torrent")
}

func envString(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
