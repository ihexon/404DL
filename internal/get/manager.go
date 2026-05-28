package get

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	dht "github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/sirupsen/logrus"

	"4dl/internal/logging"
)

var errManagerClosed = errors.New("get manager closed")

type Manager struct {
	saveTo  string
	client  *torrent.Client
	storage storage.ClientImplCloser
	runtime *runtimeCollector
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	items     map[string]*TorrentItem
	downloads map[string]*torrentDownload
}

type torrentDownload struct {
	ID             string
	Status         TorrentDownloadStatus
	Active         bool
	CompletedBytes int64
	Bytes          int64
	torrent        *torrent.Torrent
	ctx            context.Context
	cancel         context.CancelFunc
}

type torrentRuntime struct {
	t      *torrent.Torrent
	active bool
}

func NewManager(items []TorrentItem, saveTo, torrentListenAddr string) (*Manager, error) {
	if err := os.MkdirAll(saveTo, 0o750); err != nil {
		return nil, fmt.Errorf("create save directory: %w", err)
	}

	runtime := newRuntimeCollector()
	clientStorage := storage.NewFile(saveTo)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DefaultStorage = clientStorage
	cfg.Callbacks = runtime.callbacks()
	cfg.ConfigureAnacrolixDhtServer = func(cfg *dht.ServerConfig) {
		onQuery := cfg.OnQuery
		cfg.OnQuery = func(query *krpc.Msg, source net.Addr) bool {
			runtime.observeDHTQuery(query, addrString(source))
			if onQuery != nil {
				return onQuery(query, source)
			}
			return true
		}
	}
	cfg.EstablishedConnsPerTorrent = 48
	cfg.HalfOpenConnsPerTorrent = 16
	cfg.TotalHalfOpenConns = 64
	cfg.TorrentPeersHighWater = 200
	cfg.TorrentPeersLowWater = 50
	cfg.DialRateLimiter = rate.NewLimiter(50, 100)
	cfg.MaxUnverifiedBytes = 64 << 20
	cfg.PieceHashersPerTorrent = 2
	if err := configureTorrentListenAddr(cfg, torrentListenAddr); err != nil {
		_ = clientStorage.Close()
		return nil, err
	}

	client, err := torrent.NewClient(cfg)
	if err != nil {
		_ = clientStorage.Close()
		return nil, fmt.Errorf("create torrent client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		saveTo:    saveTo,
		client:    client,
		storage:   clientStorage,
		runtime:   runtime,
		ctx:       ctx,
		cancel:    cancel,
		items:     make(map[string]*TorrentItem, len(items)),
		downloads: make(map[string]*torrentDownload),
	}
	for i := range items {
		item := items[i]
		manager.items[item.ID] = &item
	}
	logrus.WithFields(logrus.Fields{
		"items":                   len(items),
		"save_to":                 saveTo,
		"torrent_listen":          torrentListenAddr,
		"torrent_peer_high":       cfg.TorrentPeersHighWater,
		"torrent_peer_low":        cfg.TorrentPeersLowWater,
		"established_per_torrent": cfg.EstablishedConnsPerTorrent,
	}).Info("get manager initialized")
	return manager, nil
}

func configureTorrentListenAddr(cfg *torrent.ClientConfig, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = defaultTorrentListenAddr
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse torrent listen address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("parse torrent listen port %q: %w", portText, err)
	}
	cfg.ListenHost = func(string) string { return host }
	cfg.ListenPort = port
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			cfg.DisableIPv6 = true
		} else {
			cfg.DisableIPv4 = true
		}
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	items := len(m.items)
	m.mu.Unlock()

	logrus.WithField("items", items).Info("get manager shutting down")
	m.cancel()
	m.wg.Wait()
	m.client.Close()
	if err := m.storage.Close(); err != nil {
		logrus.WithError(err).Error("get storage close failed")
		return err
	}
	logrus.Info("get manager shut down")
	return nil
}

func (m *Manager) List() []TorrentItem {
	m.mu.Lock()
	out := make([]TorrentItem, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, m.cloneItemLocked(item))
	}
	m.mu.Unlock()

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Seeders != out[j].Seeders {
			return out[i].Seeders > out[j].Seeders
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
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
	return m.cloneItemLocked(item), true
}

func (m *Manager) State(ctx context.Context) AppState {
	items := m.List()
	state := AppState{
		Updated:  time.Now().Format(time.RFC3339),
		SaveTo:   m.saveTo,
		Torrents: make([]TorrentState, 0, len(items)),
	}
	for _, item := range items {
		item.Files = nil
		state.Torrents = append(state.Torrents, TorrentState{
			TorrentItem: item,
			Runtime:     RuntimeView{Status: RuntimeStatusInactive},
		})
	}
	return state
}

func (m *Manager) TorrentState(id string) (TorrentState, bool) {
	item, ok := m.Get(id)
	if !ok {
		return TorrentState{}, false
	}
	item = m.refreshLoadedItem(item)
	return TorrentState{
		TorrentItem: item,
		Runtime:     m.runtimeView(item.ID),
	}, true
}

func (m *Manager) runtimeView(id string) RuntimeView {
	snapshot, ok, err := m.RuntimeSnapshotIfLoaded(id)
	if !ok {
		return RuntimeView{Status: RuntimeStatusError, Error: "torrent not found"}
	}
	if err != nil {
		return RuntimeView{Status: RuntimeStatusError, Error: err.Error()}
	}
	if snapshot == nil {
		return RuntimeView{Status: RuntimeStatusInactive}
	}
	return RuntimeView{Status: RuntimeStatusReady, Snapshot: snapshot}
}

func (m *Manager) refreshLoadedItem(item TorrentItem) TorrentItem {
	t, ok, err := m.torrentRuntime(item.ID)
	if ok && err == nil && t != nil && t.Info() != nil {
		return m.refreshFiles(item.ID, t)
	}
	return item
}

func (m *Manager) LoadedTorrent(id string) (*torrent.Torrent, bool, error) {
	hash, _, ok := m.itemTorrentSource(id)
	if !ok {
		return nil, false, nil
	}
	if hash == "" {
		return nil, true, nil
	}
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil, true, fmt.Errorf("parse info hash %q: %w", hash, err)
	}
	t, ok := m.client.Torrent(ih)
	if !ok {
		return nil, true, nil
	}
	return t, true, nil
}

func (m *Manager) RuntimeSnapshotIfLoaded(id string) (*RuntimeSnapshot, bool, error) {
	t, ok, err := m.torrentRuntime(id)
	if !ok || err != nil {
		return nil, ok, err
	}
	if t == nil {
		return nil, true, nil
	}
	snapshot := m.runtime.snapshot(id, m.client, t)
	return &snapshot, true, nil
}

func (m *Manager) FileTorrent(ctx context.Context, id string) (*torrent.Torrent, bool, error) {
	startedAt := time.Now()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, true, errManagerClosed
	}
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id": id,
		})).Warn("get torrent lookup failed: torrent not found")
		return nil, false, nil
	}
	m.wg.Add(1)
	hash := item.Hash
	magnetURL := item.MagnetURL
	fields := logging.MergeFields(ctx, logrus.Fields{
		"id":         id,
		"title":      item.Title,
		"provider":   item.Provider,
		"hash":       hash,
		"has_magnet": magnetURL != "",
	})
	m.mu.Unlock()
	defer m.wg.Done()

	logrus.WithFields(fields).Debug("get torrent metadata wait started")
	t, err := m.addTorrent(hash, magnetURL)
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("get torrent add failed")
		return nil, true, err
	}
	select {
	case <-ctx.Done():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		entry := logrus.WithError(ctx.Err()).WithFields(fields)
		if errors.Is(ctx.Err(), context.Canceled) {
			entry.Debug("get torrent metadata wait canceled")
		} else {
			entry.Warn("get torrent metadata wait canceled")
		}
		return nil, true, ctx.Err()
	case <-t.GotInfo():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		fields["info_hash"] = t.InfoHash().HexString()
		fields["files"] = len(t.Files())
		logrus.WithFields(fields).Debug("get torrent metadata ready")
		return t, true, nil
	case <-m.ctx.Done():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(errManagerClosed).WithFields(fields).Debug("get torrent metadata wait canceled")
		return nil, true, errManagerClosed
	}
}

func (m *Manager) itemTorrentSource(id string) (hash, magnetURL string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return "", "", false
	}
	return item.Hash, item.MagnetURL, true
}

func (m *Manager) StartDownload(ctx context.Context, id string) (TorrentItem, bool, error) {
	t, ok, err := m.FileTorrent(ctx, id)
	if !ok || err != nil {
		return TorrentItem{}, ok, err
	}
	m.startTorrentDownload(id, t)
	return m.refreshFiles(id, t), true, nil
}

func (m *Manager) startTorrentDownload(id string, t *torrent.Torrent) {
	m.mu.Lock()
	if current, ok := m.downloads[id]; ok && current.Active {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	if current, ok := m.downloads[id]; ok && current.cancel != nil {
		current.cancel()
	}
	m.downloads[id] = &torrentDownload{
		ID:             id,
		Status:         TorrentDownloadStatusDownloading,
		Active:         true,
		CompletedBytes: t.BytesCompleted(),
		Bytes:          t.Length(),
		torrent:        t,
		ctx:            ctx,
		cancel:         cancel,
	}
	m.mu.Unlock()

	t.AllowDataDownload()
	t.DownloadAll()
	m.monitorTorrentDownload(ctx, id, t)
}

func (m *Manager) PauseDownload(ctx context.Context, id string) (TorrentItem, bool, error) {
	runtime, ok, err := m.downloadRuntime(id)
	if !ok || err != nil {
		return TorrentItem{}, ok, err
	}
	if runtime.t == nil || !runtime.active {
		item, ok := m.Get(id)
		if !ok {
			return TorrentItem{}, false, nil
		}
		return item, true, nil
	}

	t := runtime.t
	t.CancelPieces(0, int(t.NumPieces()))
	t.DisallowDataDownload()
	m.mu.Lock()
	if current, ok := m.downloads[id]; ok {
		if current.cancel != nil {
			current.cancel()
		}
		current.Status = TorrentDownloadStatusPaused
		current.Active = false
		current.CompletedBytes = t.BytesCompleted()
		current.Bytes = t.Length()
		current.torrent = t
		current.ctx = nil
		current.cancel = nil
	}
	m.mu.Unlock()
	return m.refreshFiles(id, t), true, nil
}

func (m *Manager) DeleteDownload(ctx context.Context, id string) (TorrentItem, bool, error) {
	runtime, ok, err := m.deleteRuntime(id)
	if !ok || err != nil {
		return TorrentItem{}, ok, err
	}
	t := runtime.t
	var deleteErr error
	if t != nil {
		t.CancelPieces(0, int(t.NumPieces()))
		t.DisallowDataDownload()
		if t.Info() != nil {
			deleteErr = m.deleteTorrentFiles(t)
		} else {
			deleteErr = m.deleteItemFiles(id)
		}
		t.Drop()
	} else {
		deleteErr = m.deleteItemFiles(id)
	}
	if deleteErr != nil {
		return TorrentItem{}, true, deleteErr
	}
	return m.clearDownloadedTorrent(id), true, nil
}

func (m *Manager) monitorTorrentDownload(ctx context.Context, id string, t *torrent.Torrent) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if torrentDownloadComplete(t) {
				m.finishTorrentDownload(id, t)
				t.Drop()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.updateDownloadProgress(id, t)
			}
		}
	}()
}

func (m *Manager) updateDownloadProgress(id string, t *torrent.Torrent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dl, ok := m.downloads[id]; ok && dl.Active {
		dl.CompletedBytes = t.BytesCompleted()
		dl.Bytes = t.Length()
	}
}

func (m *Manager) finishTorrentDownload(id string, t *torrent.Torrent) {
	files, _ := m.fileItems(t, false)
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.downloads[id]; ok {
		current.Status = TorrentDownloadStatusComplete
		current.Active = false
		current.CompletedBytes = t.BytesCompleted()
		current.Bytes = t.Length()
		current.torrent = nil
		current.ctx = nil
		current.cancel = nil
	}
	if item, ok := m.items[id]; ok {
		item.Error = ""
		item.Files = files
		item.Downloading = false
		item.Download = m.downloadViewLocked(id, t)
	}
}

func (m *Manager) deleteItemFiles(id string) error {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok || len(item.Files) == 0 {
		m.mu.Unlock()
		return nil
	}
	paths := make([]string, 0, len(item.Files)*2)
	for _, file := range item.Files {
		paths = append(paths, file.SavePath, file.SavePath+".part")
	}
	m.mu.Unlock()
	for _, path := range paths {
		if err := removeSavedPath(m.saveTo, path); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) deleteTorrentFiles(t *torrent.Torrent) error {
	paths := make([]string, 0, len(t.Files())*2)
	for _, file := range t.Files() {
		paths = append(paths, file.Path(), file.Path()+".part")
	}
	for _, path := range paths {
		if err := removeSavedPath(m.saveTo, path); err != nil {
			return err
		}
	}
	return nil
}

func removeSavedPath(root, path string) error {
	fullPath, err := savedPath(root, path)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete saved file %q: %w", fullPath, err)
	}
	return nil
}

func savedPath(root, path string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve save directory %q: %w", root, err)
	}
	root = filepath.Clean(root)
	path = filepath.Clean(filepath.FromSlash(path))

	fullPath := path
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(root, path)
	}
	fullPath = filepath.Clean(fullPath)

	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve saved path %q: %w", path, err)
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refuse to delete outside save directory: %q", fullPath)
	}
	return fullPath, nil
}

func torrentDownloadComplete(t *torrent.Torrent) bool {
	return t.Length() == 0 || t.BytesCompleted() >= t.Length()
}

func (m *Manager) refreshFiles(id string, t *torrent.Torrent) TorrentItem {
	downloading := m.torrentDownloading(id)
	files, _ := m.fileItems(t, downloading)
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return TorrentItem{}
	}
	item.Error = ""
	item.Files = files
	item.Downloading = downloading
	item.Download = m.downloadViewLocked(id, t)
	return m.cloneItemLocked(item)
}

func (m *Manager) clearDownloadedTorrent(id string) TorrentItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return TorrentItem{}
	}
	for i := range item.Files {
		item.Files[i].CompletedBytes = 0
		item.Files[i].Status = FileStatusIdle
	}
	item.Downloading = false
	item.Download = DownloadView{Status: TorrentDownloadStatusIdle, Bytes: item.Bytes}
	return m.cloneItemLocked(item)
}

func (m *Manager) fileItems(t *torrent.Torrent, downloading bool) ([]FileItem, int64) {
	files := make([]FileItem, 0, len(t.Files()))
	totalBytes := int64(0)
	for _, file := range t.Files() {
		totalBytes += file.Length()
		bytesCompleted := file.BytesCompleted()
		files = append(files, FileItem{
			Path:           file.DisplayPath(),
			Bytes:          file.Length(),
			CompletedBytes: bytesCompleted,
			SavePath:       filepath.Join(m.saveTo, filepath.FromSlash(file.Path())),
			Status:         fileStatus(file.Length(), bytesCompleted, downloading),
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, totalBytes
}

func fileStatus(length, bytesCompleted int64, downloading bool) FileStatus {
	switch {
	case length == 0 || bytesCompleted >= length:
		return FileStatusComplete
	case downloading:
		return FileStatusDownloading
	default:
		return FileStatusIdle
	}
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

func (m *Manager) torrentRuntime(id string) (*torrent.Torrent, bool, error) {
	m.mu.Lock()
	if _, ok := m.items[id]; !ok {
		m.mu.Unlock()
		return nil, false, nil
	} else if download, ok := m.downloads[id]; ok && download.torrent != nil {
		t := download.torrent
		m.mu.Unlock()
		return t, true, nil
	}
	m.mu.Unlock()
	return m.LoadedTorrent(id)
}

func (m *Manager) downloadRuntime(id string) (torrentRuntime, bool, error) {
	m.mu.Lock()
	if _, ok := m.items[id]; !ok {
		m.mu.Unlock()
		return torrentRuntime{}, false, nil
	}
	if download, ok := m.downloads[id]; ok {
		out := torrentRuntime{t: download.torrent, active: download.Active}
		m.mu.Unlock()
		return out, true, nil
	}
	m.mu.Unlock()
	t, ok, err := m.LoadedTorrent(id)
	return torrentRuntime{t: t}, ok, err
}

func (m *Manager) deleteRuntime(id string) (torrentRuntime, bool, error) {
	m.mu.Lock()
	if _, ok := m.items[id]; !ok {
		m.mu.Unlock()
		return torrentRuntime{}, false, nil
	}
	if download, ok := m.downloads[id]; ok {
		out := torrentRuntime{t: download.torrent, active: download.Active}
		if download.cancel != nil {
			download.cancel()
		}
		delete(m.downloads, id)
		m.mu.Unlock()
		return out, true, nil
	}
	m.mu.Unlock()
	t, ok, err := m.LoadedTorrent(id)
	return torrentRuntime{t: t}, ok, err
}

func (m *Manager) cloneItemLocked(item *TorrentItem) TorrentItem {
	out := *item
	out.Downloading = m.torrentDownloadingLocked(item.ID)
	out.Download = m.downloadViewLocked(item.ID, nil)
	if item.Files != nil {
		out.Files = append([]FileItem(nil), item.Files...)
	}
	return out
}

func (m *Manager) torrentDownloadingLocked(id string) bool {
	download, ok := m.downloads[id]
	return ok && download.Active
}

func (m *Manager) torrentDownloading(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.torrentDownloadingLocked(id)
}

func (m *Manager) downloadViewLocked(id string, t *torrent.Torrent) DownloadView {
	if download, ok := m.downloads[id]; ok {
		completed := download.CompletedBytes
		bytes := download.Bytes
		if t != nil && t.Info() != nil {
			completed = t.BytesCompleted()
			bytes = t.Length()
		}
		return DownloadView{
			Status:         download.Status,
			CompletedBytes: completed,
			Bytes:          bytes,
		}
	}
	item, ok := m.items[id]
	if !ok {
		return DownloadView{Status: TorrentDownloadStatusIdle}
	}
	if item.Download.Status != "" {
		return item.Download
	}
	return DownloadView{Status: TorrentDownloadStatusIdle, Bytes: item.Bytes}
}
