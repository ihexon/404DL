package downloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	log "github.com/sirupsen/logrus"
)

type addTorrentFunc func(*torrent.Client) (*torrent.Torrent, error)

func DownloadMagnet(ctx context.Context, magnetURL string, cfg Config) error {
	if !strings.HasPrefix(magnetURL, "magnet:") {
		return fmt.Errorf("value is not a magnet URL")
	}

	return download(ctx, cfg, func(client *torrent.Client) (*torrent.Torrent, error) {
		t, err := client.AddMagnet(magnetURL)
		if err != nil {
			return nil, fmt.Errorf("add magnet: %w", err)
		}
		return t, nil
	})
}

func DownloadTorrentFile(ctx context.Context, torrentPath string, cfg Config) error {
	if strings.TrimSpace(torrentPath) == "" {
		return fmt.Errorf("torrent file path is required")
	}

	return download(ctx, cfg, func(client *torrent.Client) (*torrent.Torrent, error) {
		t, err := client.AddTorrentFromFile(torrentPath)
		if err != nil {
			return nil, fmt.Errorf("add torrent file: %w", err)
		}
		return t, nil
	})
}

func download(ctx context.Context, cfg Config, addTorrent addTorrentFunc) (err error) {
	if err := cfg.Validate(); err != nil {
		return err
	}

	client, clientStorage, err := newTorrentClient(cfg)
	if err != nil {
		return err
	}

	interrupted := false
	defer func() {
		closeTorrentResources(client, clientStorage, closeTimeout(cfg, interrupted))
	}()

	status, err := serveStatus(ctx, client, cfg.StatusAddr)
	if err != nil {
		return err
	}
	defer closeStatusServer(status)

	t, err := addTorrent(client)
	if err != nil {
		return err
	}

	if err := waitForInfo(ctx, t); err != nil {
		interrupted = true
		return err
	}

	t.DownloadAll()
	return waitForDownload(ctx, client, t, cfg, &interrupted)
}

func waitForInfo(ctx context.Context, t *torrent.Torrent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.GotInfo():
		return nil
	}
}

func waitForDownload(
	ctx context.Context,
	client *torrent.Client,
	t *torrent.Torrent,
	cfg Config,
	interrupted *bool,
) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.WaitAll()
	}()

	interval := cfg.ProgressInterval
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	reporter := newProgressReporter(t)
	writeProgress(cfg.ProgressWriter, reporter.progress())

	for {
		select {
		case <-ctx.Done():
			*interrupted = true
			return ctx.Err()
		case <-done:
			writeProgress(cfg.ProgressWriter, reporter.progress())
			return nil
		case <-ticker.C:
			writeProgress(cfg.ProgressWriter, reporter.progress())
		}
	}
}

func closeTimeout(cfg Config, interrupted bool) time.Duration {
	if !interrupted {
		return 0
	}
	if cfg.CloseTimeout > 0 {
		return cfg.CloseTimeout
	}
	return 2 * time.Second
}

func closeTorrentResources(client *torrent.Client, clientStorage storage.ClientImplCloser, timeout time.Duration) {
	if timeout <= 0 {
		closeTorrentClient(client)
		closeDownloadStorage(clientStorage)
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		closeTorrentClient(client)
		closeDownloadStorage(clientStorage)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.WithField("timeout", timeout.String()).Warn("torrent close timed out")
	}
}

func closeTorrentClient(client *torrent.Client) {
	if errs := client.Close(); len(errs) > 0 {
		log.WithError(errors.Join(errs...)).Warn("close torrent client")
	}
}

func closeDownloadStorage(clientStorage storage.ClientImplCloser) {
	if err := clientStorage.Close(); err != nil {
		log.WithError(err).Warn("close torrent storage")
	}
}

func closeStatusServer(server *statusServer) {
	if server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Close(ctx); err != nil {
		log.WithError(err).Warn("close status server")
	}
}
