package httpfs

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/sirupsen/logrus"

	"mvdl/internal/logging"
)

const metadataTimeout = 2 * time.Minute

type Manager struct {
	baseURL string
	client  *torrent.Client
	storage storage.ClientImplCloser

	mu    sync.Mutex
	items map[string]*TorrentItem
}

func NewManager(items []TorrentItem, dataDir, listenAddr, torrentListenAddr string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	clientStorage := storage.NewFileByInfoHash(filepath.Join(dataDir, "pieces"))
	cfg := torrent.NewDefaultClientConfig()
	cfg.DefaultStorage = clientStorage
	cfg.EstablishedConnsPerTorrent = 200
	cfg.HalfOpenConnsPerTorrent = 100
	cfg.TotalHalfOpenConns = 400
	cfg.TorrentPeersHighWater = 1000
	cfg.TorrentPeersLowWater = 200
	cfg.DialRateLimiter = rate.NewLimiter(100, 200)
	cfg.MaxUnverifiedBytes = 512 << 20
	cfg.PieceHashersPerTorrent = 8
	cfg.SetListenAddr(torrentListenAddr)

	client, err := torrent.NewClient(cfg)
	if err != nil {
		_ = clientStorage.Close()
		return nil, fmt.Errorf("create torrent client: %w", err)
	}

	manager := &Manager{
		baseURL: publicBaseURL(listenAddr),
		client:  client,
		storage: clientStorage,
		items:   make(map[string]*TorrentItem, len(items)),
	}
	for i := range items {
		item := items[i]
		manager.items[item.ID] = &item
	}
	logrus.WithFields(logrus.Fields{
		"items":                   len(items),
		"data_dir":                dataDir,
		"piece_dir":               filepath.Join(dataDir, "pieces"),
		"public_base_url":         manager.baseURL,
		"torrent_listen":          torrentListenAddr,
		"torrent_peer_high":       cfg.TorrentPeersHighWater,
		"torrent_peer_low":        cfg.TorrentPeersLowWater,
		"established_per_torrent": cfg.EstablishedConnsPerTorrent,
	}).Info("httpfs manager initialized")
	return manager, nil
}

func (m *Manager) Close() error {
	logrus.WithField("items", len(m.items)).Info("httpfs manager shutting down")
	m.client.Close()
	if err := m.storage.Close(); err != nil {
		logrus.WithError(err).Error("httpfs storage close failed")
		return err
	}
	logrus.Info("httpfs manager shut down")
	return nil
}

func (m *Manager) List() []TorrentItem {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]TorrentItem, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, cloneItem(item))
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i].Seeders
		right := out[j].Seeders
		if left == right {
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		}
		return left > right
	})
	return out
}

func (m *Manager) Get(id string) (TorrentItem, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[id]
	if !ok {
		return TorrentItem{}, false
	}
	return cloneItem(item), true
}

func (m *Manager) EnsureFiles(ctx context.Context, id string) (TorrentItem, bool) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id": id,
		})).Warn("httpfs metadata request failed: torrent not found")
		return TorrentItem{}, false
	}
	if item.Status == TorrentStatusIdle {
		item.Status = TorrentStatusLoading
		item.Error = ""
		item.Files = nil
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id":         id,
			"title":      item.Title,
			"provider":   item.Provider,
			"hash":       item.Hash,
			"has_magnet": item.MagnetURL != "",
			"status":     item.Status,
		})).Info("httpfs metadata loading scheduled")
		go m.loadMetadata(id, logging.RequestID(ctx))
	}
	out := cloneItem(item)
	m.mu.Unlock()
	return out, true
}

func (m *Manager) FileTorrent(ctx context.Context, id string) (*torrent.Torrent, bool, error) {
	startedAt := time.Now()
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id": id,
		})).Warn("httpfs torrent lookup failed: torrent not found")
		return nil, false, nil
	}
	hash := item.Hash
	magnetURL := item.MagnetURL
	fields := logging.MergeFields(ctx, logrus.Fields{
		"id":         id,
		"title":      item.Title,
		"provider":   item.Provider,
		"hash":       hash,
		"has_magnet": magnetURL != "",
		"status":     item.Status,
	})
	m.mu.Unlock()

	logrus.WithFields(fields).Info("httpfs torrent metadata wait started")
	t, err := m.addTorrent(hash, magnetURL)
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("httpfs torrent add failed")
		return nil, true, err
	}
	select {
	case <-ctx.Done():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(ctx.Err()).WithFields(fields).Warn("httpfs torrent metadata wait canceled")
		return nil, true, ctx.Err()
	case <-t.GotInfo():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		fields["info_hash"] = t.InfoHash().HexString()
		fields["files"] = len(t.Files())
		logrus.WithFields(fields).Info("httpfs torrent metadata ready")
		return t, true, nil
	}
}

func (m *Manager) loadMetadata(id, requestID string) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()
	ctx = logging.WithRequestID(ctx, requestID)

	t, ok, err := m.FileTorrent(ctx, id)
	if !ok {
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id": id,
		})).Warn("httpfs metadata load stopped: torrent not found")
		return
	}
	if err != nil {
		m.setError(id, err)
		logrus.WithError(err).WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id":          id,
			"duration_ms": logging.DurationMillis(time.Since(startedAt)),
		})).Error("httpfs metadata load failed")
		return
	}

	files := make([]FileItem, 0, len(t.Files()))
	totalBytes := int64(0)
	for _, file := range t.Files() {
		path := file.DisplayPath()
		totalBytes += file.Length()
		files = append(files, FileItem{
			Path:        path,
			Bytes:       file.Length(),
			DownloadURL: m.downloadURL(id, path),
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	m.mu.Lock()
	if item, ok := m.items[id]; ok {
		setFinalStatus(item, TorrentStatusReady, "", files)
	}
	m.mu.Unlock()
	logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
		"id":          id,
		"files":       len(files),
		"bytes":       totalBytes,
		"duration_ms": logging.DurationMillis(time.Since(startedAt)),
	})).Info("httpfs metadata load completed")
}

func (m *Manager) addTorrent(hash, magnetURL string) (*torrent.Torrent, error) {
	if magnetURL != "" {
		t, err := m.client.AddMagnet(magnetURL)
		if err != nil {
			return nil, fmt.Errorf("add magnet: %w", err)
		}
		return t, nil
	}

	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil, fmt.Errorf("parse info hash %q: %w", hash, err)
	}
	t, _ := m.client.AddTorrentInfoHash(ih)
	return t, nil
}

func (m *Manager) setError(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item, ok := m.items[id]; ok {
		setFinalStatus(item, TorrentStatusError, err.Error(), nil)
		logrus.WithError(err).WithFields(logrus.Fields{
			"id":       id,
			"title":    item.Title,
			"provider": item.Provider,
			"status":   TorrentStatusError,
		}).Error("httpfs torrent status set to error")
	}
}

func setFinalStatus(item *TorrentItem, status TorrentStatus, message string, files []FileItem) {
	if isFinalStatus(item.Status) {
		return
	}
	item.Status = status
	item.Error = message
	item.Files = files
}

func isFinalStatus(status TorrentStatus) bool {
	return status == TorrentStatusReady ||
		status == TorrentStatusError ||
		status == TorrentStatusUnavailable
}

func (m *Manager) downloadURL(id, filePath string) string {
	return m.baseURL + "/d/" + url.PathEscape(id) + "/" + pathEscape(filePath)
}

func publicBaseURL(listenAddr string) string {
	host := listenAddr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if strings.HasPrefix(host, "0.0.0.0:") {
		host = "127.0.0.1:" + strings.TrimPrefix(host, "0.0.0.0:")
	}
	return "http://" + host
}

func pathEscape(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func cloneItem(item *TorrentItem) TorrentItem {
	out := *item
	if item.Files != nil {
		out.Files = append([]FileItem(nil), item.Files...)
	}
	return out
}
