package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type Config struct {
	DataDir          string
	ProgressInterval time.Duration
	ProgressWriter   io.Writer
	StatusAddr       string
	CloseTimeout     time.Duration
	HTTPTimeout      time.Duration

	EstablishedConnsPerTorrent int
	HalfOpenConnsPerTorrent    int
	TotalHalfOpenConns         int
	TorrentPeersHighWater      int
	TorrentPeersLowWater       int
	MaxUnverifiedBytes         int64
	MaxPeerRequestBufferBytes  int
	DialRateLimit              float64
	DialRateBurst              int
	PieceHashersPerTorrent     int
	NoUpload                   bool
	Seed                       bool
	DisableIPv6                bool
	ListenAddr                 string
	DownloadRateBytesPerSec    int
	UploadRateBytesPerSec      int
}

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
	clientConfig := torrent.NewDefaultClientConfig()
	applyClientConfig(clientConfig, cfg)

	downloadStorage := storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: cfg.DataDir,
		UsePartFiles:  g.Some(false),
		Logger:        slog.New(logrusSlogHandler{minLevel: slog.LevelError}),
	})
	clientConfig.DefaultStorage = downloadStorage

	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		closeDownloadStorage(downloadStorage)
		return fmt.Errorf("create torrent client: %w", err)
	}
	interrupted := false
	defer func() {
		timeout := time.Duration(0)
		if interrupted {
			timeout = cfg.CloseTimeout
			if timeout <= 0 {
				timeout = 2 * time.Second
			}
		}
		closeTorrentResources(client, downloadStorage, timeout)
	}()
	if err := serveStatus(ctx, client, cfg.StatusAddr); err != nil {
		return err
	}

	t, err := addTorrent(client)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		interrupted = true
		return ctx.Err()
	case <-t.GotInfo():
	}

	t.DownloadAll()

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

	select {
	case <-ctx.Done():
		interrupted = true
		return ctx.Err()
	case <-done:
		writeProgress(cfg.ProgressWriter, reporter.progress())
		return nil
	case <-ticker.C:
	}

	for {
		writeProgress(cfg.ProgressWriter, reporter.progress())

		select {
		case <-ctx.Done():
			interrupted = true
			return ctx.Err()
		case <-done:
			writeProgress(cfg.ProgressWriter, reporter.progress())
			return nil
		case <-ticker.C:
		}
	}
}

func closeTorrentResources(client *torrent.Client, downloadStorage storage.ClientImplCloser, timeout time.Duration) {
	if timeout <= 0 {
		closeTorrentClient(client)
		closeDownloadStorage(downloadStorage)
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		closeTorrentClient(client)
		closeDownloadStorage(downloadStorage)
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

func closeDownloadStorage(downloadStorage storage.ClientImplCloser) {
	if err := downloadStorage.Close(); err != nil {
		log.WithError(err).Warn("close torrent storage")
	}
}

func serveStatus(ctx context.Context, client *torrent.Client, addr string) error {
	if strings.TrimSpace(addr) == "" {
		return nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen status server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		client.WriteStatus(w)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		client.WriteStatus(w)
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			_, _ = fmt.Fprintf(io.Discard, "status server stopped: %v", err)
		}
	}()
	return nil
}

func applyClientConfig(clientConfig *torrent.ClientConfig, cfg Config) {
	clientConfig.DataDir = cfg.DataDir
	clientConfig.Slogger = slog.New(logrusSlogHandler{minLevel: slog.LevelError})
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
		burst := cfg.DialRateBurst
		if burst <= 0 {
			burst = int(cfg.DialRateLimit)
		}
		clientConfig.DialRateLimiter = rate.NewLimiter(rate.Limit(cfg.DialRateLimit), burst)
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

type progress struct {
	CompletedBytes int64
	TotalBytes     int64
	DownloadRate   int64
}

type progressReporter struct {
	t         *torrent.Torrent
	lastAt    time.Time
	lastStats torrent.TorrentStats
}

func newProgressReporter(t *torrent.Torrent) *progressReporter {
	return &progressReporter{
		t:         t,
		lastAt:    time.Now(),
		lastStats: t.Stats(),
	}
}

func (r *progressReporter) progress() progress {
	now := time.Now()
	stats := r.t.Stats()
	interval := now.Sub(r.lastAt)
	if interval <= 0 {
		interval = time.Second
	}

	rate := bytesPerSecond(
		stats.BytesReadUsefulData.Int64()-r.lastStats.BytesReadUsefulData.Int64(),
		interval,
	)

	r.lastAt = now
	r.lastStats = stats

	return progress{
		CompletedBytes: r.t.BytesCompleted(),
		TotalBytes:     r.t.Info().TotalLength(),
		DownloadRate:   rate,
	}
}

func bytesPerSecond(bytes int64, interval time.Duration) int64 {
	return bytes * int64(time.Second) / int64(interval)
}

func writeProgress(w io.Writer, p progress) {
	if w == nil {
		return
	}
	fmt.Fprintf(
		w,
		"total: %s, downloaded: %s, speed: %s/s\n",
		humanize.Bytes(uint64(p.TotalBytes)),
		humanize.Bytes(uint64(p.CompletedBytes)),
		humanize.Bytes(uint64(p.DownloadRate)),
	)
}

type logrusSlogHandler struct {
	attrs    []slog.Attr
	groups   []string
	minLevel slog.Level
}

func (h logrusSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h logrusSlogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := log.Fields{}
	for _, attr := range h.attrs {
		addSlogAttr(fields, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addSlogAttr(fields, h.groups, attr)
		return true
	})

	entry := log.WithFields(fields)
	switch {
	case record.Level >= slog.LevelError:
		entry.Error(record.Message)
	case record.Level >= slog.LevelWarn:
		entry.Warn(record.Message)
	case record.Level <= slog.LevelDebug:
		entry.Debug(record.Message)
	default:
		entry.Info(record.Message)
	}
	return nil
}

func (h logrusSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := logrusSlogHandler{
		attrs:    append(append([]slog.Attr{}, h.attrs...), attrs...),
		groups:   append([]string{}, h.groups...),
		minLevel: h.minLevel,
	}
	return next
}

func (h logrusSlogHandler) WithGroup(name string) slog.Handler {
	next := logrusSlogHandler{
		attrs:    append([]slog.Attr{}, h.attrs...),
		groups:   append(append([]string{}, h.groups...), name),
		minLevel: h.minLevel,
	}
	return next
}

func addSlogAttr(fields log.Fields, groups []string, attr slog.Attr) {
	if attr.Key == "" {
		return
	}
	value := attr.Value.Resolve()
	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(append(append([]string{}, groups...), attr.Key), ".")
	}
	fields[key] = value.Any()
}
