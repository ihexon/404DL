package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/crypto"
	"mvdl/internal/downloader"
)

type downloadRequest struct {
	Kind string
	Run  func() error
}

func runDownload(c *cli.Context) error {
	value := c.Args().First()
	if value == "" {
		return fmt.Errorf("magnet URL or torrent file is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := newDownloadConfig(c)
	req, err := newDownloadRequest(ctx, value, cfg)
	if err != nil {
		return err
	}

	logDownloadStart(req.Kind, cfg)
	if err := req.Run(); err != nil {
		if errors.Is(err, context.Canceled) {
			logrus.WithField("saveTo", cfg.DataDir).Info("download interrupted")
			return nil
		}
		return err
	}
	logrus.WithField("saveTo", cfg.DataDir).Info("download completed")
	return nil
}

func newDownloadConfig(c *cli.Context) downloader.Config {
	return downloader.Config{
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
}

func newDownloadRequest(ctx context.Context, value string, cfg downloader.Config) (downloadRequest, error) {
	if isTorrentFile(value) {
		return downloadRequest{
			Kind: "torrent",
			Run: func() error {
				return downloader.DownloadTorrentFile(ctx, value, cfg)
			},
		}, nil
	}

	magnetURL, err := resolveMagnetURL(value)
	if err != nil {
		return downloadRequest{}, err
	}
	return downloadRequest{
		Kind: "magnet",
		Run: func() error {
			return downloader.DownloadMagnet(ctx, magnetURL, cfg)
		},
	}, nil
}

func logDownloadStart(kind string, cfg downloader.Config) {
	logrus.WithFields(logrus.Fields{
		"saveTo":      cfg.DataDir,
		"type":        kind,
		"status":      cfg.StatusAddr,
		"connections": cfg.EstablishedConnsPerTorrent,
		"halfOpen":    cfg.HalfOpenConnsPerTorrent,
		"dialRate":    cfg.DialRateLimit,
	}).Info("download started")
}

func resolveMagnetURL(value string) (string, error) {
	if isMagnetURL(value) {
		return value, nil
	}

	key := envString(envCryptoKey, "")
	if key == "" {
		return "", fmt.Errorf("%s is required to decrypt encrypted magnet URL", envCryptoKey)
	}

	decryptor, err := crypto.NewStringEncryptor(key)
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
