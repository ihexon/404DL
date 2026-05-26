package downloader

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

func newTorrentClient(cfg Config) (*torrent.Client, storage.ClientImplCloser, error) {
	clientStorage := storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: cfg.DataDir,
		UsePartFiles:  g.Some(false),
		Logger:        newTorrentLogger(),
	})

	clientConfig := torrent.NewDefaultClientConfig()
	applyClientConfig(clientConfig, cfg, clientStorage)

	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		closeDownloadStorage(clientStorage)
		return nil, nil, fmt.Errorf("create torrent client: %w", err)
	}
	return client, clientStorage, nil
}

func applyClientConfig(
	clientConfig *torrent.ClientConfig,
	cfg Config,
	clientStorage storage.ClientImplCloser,
) {
	clientConfig.DataDir = cfg.DataDir
	clientConfig.DefaultStorage = clientStorage
	clientConfig.Slogger = newTorrentLogger()
	clientConfig.WebTransport = torrentHTTPTransport(cfg)

	if cfg.EstablishedConnsPerTorrent > 0 {
		clientConfig.EstablishedConnsPerTorrent = cfg.EstablishedConnsPerTorrent
	}
	if cfg.HalfOpenConnsPerTorrent > 0 {
		clientConfig.HalfOpenConnsPerTorrent = cfg.HalfOpenConnsPerTorrent
	}
	if cfg.TotalHalfOpenConns > 0 {
		clientConfig.TotalHalfOpenConns = cfg.TotalHalfOpenConns
	}
	if cfg.TorrentPeersHighWater > 0 {
		clientConfig.TorrentPeersHighWater = cfg.TorrentPeersHighWater
	}
	if cfg.TorrentPeersLowWater > 0 {
		clientConfig.TorrentPeersLowWater = cfg.TorrentPeersLowWater
	}
	if cfg.MaxUnverifiedBytes > 0 {
		clientConfig.MaxUnverifiedBytes = cfg.MaxUnverifiedBytes
	}
	if cfg.MaxPeerRequestBufferBytes > 0 {
		clientConfig.MaxAllocPeerRequestDataPerConn = cfg.MaxPeerRequestBufferBytes
	}
	if cfg.DialRateLimit > 0 {
		clientConfig.DialRateLimiter = rate.NewLimiter(
			rate.Limit(cfg.DialRateLimit),
			dialRateBurst(cfg),
		)
	}
	if cfg.PieceHashersPerTorrent > 0 {
		clientConfig.PieceHashersPerTorrent = cfg.PieceHashersPerTorrent
	}
	if cfg.ListenAddr != "" {
		clientConfig.SetListenAddr(cfg.ListenAddr)
	}
	if cfg.DownloadRateBytesPerSec > 0 {
		clientConfig.DownloadRateLimiter = rate.NewLimiter(rate.Limit(cfg.DownloadRateBytesPerSec), 0)
	}
	if cfg.UploadRateBytesPerSec > 0 {
		clientConfig.UploadRateLimiter = rate.NewLimiter(
			rate.Limit(cfg.UploadRateBytesPerSec),
			max(cfg.MaxPeerRequestBufferBytes, 256<<10),
		)
	}

	clientConfig.NoUpload = cfg.NoUpload
	clientConfig.Seed = cfg.Seed
	clientConfig.DisableIPv6 = cfg.DisableIPv6
}

func dialRateBurst(cfg Config) int {
	if cfg.DialRateBurst > 0 {
		return cfg.DialRateBurst
	}
	return int(cfg.DialRateLimit)
}

func torrentHTTPTransport(cfg Config) http.RoundTripper {
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	dialer := &net.Dialer{Timeout: timeout}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		MaxConnsPerHost:       10,
	}
}

func newTorrentLogger() *slog.Logger {
	return slog.New(logrusSlogHandler{minLevel: slog.LevelError})
}
