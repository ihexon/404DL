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

	"mvdl/internal/logging"
)

const metadataTimeout = 2 * time.Minute

var errManagerClosed = errors.New("get manager closed")

type Manager struct {
	saveTo  string
	client  *torrent.Client
	storage storage.ClientImplCloser
	runtime *runtimeCollector
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	mu     sync.Mutex
	closed bool
	items  map[string]*TorrentItem
	tasks  map[string]*downloadTask
	nextID uint64
}

type downloadTask struct {
	TaskID string
	ID     string
	Path   string
	Token  uint64
	Status DownloadTaskStatus
	Active bool
	ctx    context.Context
	cancel context.CancelFunc
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
	cfg.EstablishedConnsPerTorrent = 200
	cfg.HalfOpenConnsPerTorrent = 100
	cfg.TotalHalfOpenConns = 400
	cfg.TorrentPeersHighWater = 1000
	cfg.TorrentPeersLowWater = 200
	cfg.DialRateLimiter = rate.NewLimiter(100, 200)
	cfg.MaxUnverifiedBytes = 512 << 20
	cfg.PieceHashersPerTorrent = 8
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
		saveTo:  saveTo,
		client:  client,
		storage: clientStorage,
		runtime: runtime,
		ctx:     ctx,
		cancel:  cancel,
		items:   make(map[string]*TorrentItem, len(items)),
		tasks:   make(map[string]*downloadTask),
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
	if item.Status == TorrentStatusReady {
		item = m.refreshReadyItem(item)
	}
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

func (m *Manager) refreshReadyItem(item TorrentItem) TorrentItem {
	t, ok, err := m.LoadedTorrent(item.ID)
	if ok && err == nil && t != nil && t.Info() != nil {
		return m.refreshFiles(item.ID, t)
	}
	return item
}

func (m *Manager) TaskTorrentID(taskID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, task, ok := m.findTaskByIDLocked(taskID)
	if !ok {
		return "", false
	}
	return task.ID, true
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
	t, ok, err := m.LoadedTorrent(id)
	if !ok || err != nil {
		return nil, ok, err
	}
	if t == nil {
		return nil, true, nil
	}
	snapshot := m.runtime.snapshot(id, m.client, t)
	return &snapshot, true, nil
}

func (m *Manager) RuntimeSnapshot(id string) (RuntimeSnapshot, bool, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return RuntimeSnapshot{}, true, errManagerClosed
	}
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return RuntimeSnapshot{}, false, nil
	}
	m.wg.Add(1)
	hash := item.Hash
	magnetURL := item.MagnetURL
	m.mu.Unlock()
	defer m.wg.Done()

	t, err := m.addTorrent(hash, magnetURL)
	if err != nil {
		return RuntimeSnapshot{}, true, err
	}
	return m.runtime.snapshot(id, m.client, t), true, nil
}

func (m *Manager) EnsureFiles(ctx context.Context, id string) (TorrentItem, bool) {
	out, ok, scheduled := m.beginMetadataLoad(ctx, id, "request")
	if scheduled {
		go func(requestID string) {
			defer m.wg.Done()
			m.loadMetadata(id, requestID)
		}(logging.RequestID(ctx))
	}
	if !ok || scheduled || out.Status != TorrentStatusReady {
		return out, ok
	}

	t, loaded, err := m.LoadedTorrent(id)
	if loaded && err == nil && t != nil && t.Info() != nil {
		return m.refreshFiles(id, t), true
	}
	return out, true
}

func (m *Manager) beginMetadataLoad(ctx context.Context, id, source string) (TorrentItem, bool, bool) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id": id,
		})).Warn("get metadata request failed: torrent not found")
		return TorrentItem{}, false, false
	}
	shouldLoadMetadata := item.Status == TorrentStatusIdle && !m.closed
	if shouldLoadMetadata {
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
			"source":     source,
		})).Debug("get metadata loading scheduled")
		m.wg.Add(1)
	}
	out := m.cloneItemLocked(item)
	m.mu.Unlock()
	return out, true, shouldLoadMetadata
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
		"status":     item.Status,
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

func (m *Manager) loadMetadata(id, requestID string) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(m.ctx, metadataTimeout)
	defer cancel()
	ctx = logging.WithRequestID(ctx, requestID)

	t, ok, err := m.FileTorrent(ctx, id)
	if !ok {
		logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id": id,
		})).Warn("get metadata load stopped: torrent not found")
		return
	}
	if err != nil {
		if isMetadataLoadCanceled(err) {
			logMetadataLoadCanceled(ctx, id, startedAt, err)
			return
		}
		m.setError(id, err)
		logrus.WithError(err).WithFields(logging.MergeFields(ctx, logrus.Fields{
			"id":          id,
			"duration_ms": logging.DurationMillis(time.Since(startedAt)),
		})).Error("get metadata load failed")
		return
	}
	if err := ctx.Err(); err != nil {
		logMetadataLoadCanceled(ctx, id, startedAt, err)
		return
	}

	files, totalBytes := m.fileItems(id, t)
	if err := ctx.Err(); err != nil {
		logMetadataLoadCanceled(ctx, id, startedAt, err)
		return
	}

	m.mu.Lock()
	if item, ok := m.items[id]; ok {
		setFinalStatus(item, TorrentStatusReady, "", files)
		item.Downloading = m.torrentDownloadingLocked(id)
	}
	m.mu.Unlock()
	logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
		"id":          id,
		"files":       len(files),
		"bytes":       totalBytes,
		"duration_ms": logging.DurationMillis(time.Since(startedAt)),
	})).Debug("get metadata load completed")
}

func (m *Manager) DownloadFile(ctx context.Context, id, filePath string) (TorrentItem, bool, error) {
	if filePath == "" {
		return TorrentItem{}, true, fmt.Errorf("file path is required")
	}
	t, ok, err := m.FileTorrent(ctx, id)
	if !ok || err != nil {
		return TorrentItem{}, ok, err
	}
	file := findTorrentFile(t, filePath)
	if file == nil {
		return TorrentItem{}, true, fmt.Errorf("file not found")
	}
	m.startDownloadFile(id, filePath, t)
	return m.refreshFiles(id, t), true, nil
}

func (m *Manager) startDownloadFile(id, filePath string, t *torrent.Torrent) {
	key := activeDownloadKey(id, filePath)
	m.mu.Lock()
	if current, ok := m.tasks[key]; ok {
		if current.Status == DownloadTaskStatusComplete || current.Active {
			m.mu.Unlock()
			return
		}
		if current.cancel != nil {
			current.cancel()
		}
	}
	m.nextID++
	token := m.nextID
	ctx, cancel := context.WithCancel(m.ctx)
	m.tasks[key] = &downloadTask{
		TaskID: fmt.Sprintf("%s:%d", id, token),
		ID:     id,
		Path:   filePath,
		Token:  token,
		Status: DownloadTaskStatusDownloading,
		Active: true,
		ctx:    ctx,
		cancel: cancel,
	}
	m.mu.Unlock()

	m.applyDownloadPlan(id, t)
	m.monitorDownloadFile(ctx, id, key, token, t, filePath)
}

func (m *Manager) PauseDownload(ctx context.Context, taskID string) (bool, error) {
	task, ok := m.updateTask(ctx, taskID, func(task *downloadTask) {
		if task.Status == DownloadTaskStatusComplete {
			return
		}
		if task.cancel != nil {
			task.cancel()
		}
		task.Status = DownloadTaskStatusPaused
		task.Active = false
		task.ctx = nil
		task.cancel = nil
	})
	if !ok {
		return false, nil
	}
	return true, m.applyTaskTorrentPlan(ctx, task)
}

func (m *Manager) ResumeDownload(ctx context.Context, taskID string) (bool, error) {
	task, ok := m.resumeTask(taskID)
	if !ok {
		return false, nil
	}
	if task.Status == DownloadTaskStatusComplete && !task.Active {
		return true, nil
	}
	t, ok, err := m.FileTorrent(ctx, task.ID)
	if !ok || err != nil {
		return ok, err
	}
	if task.Path != "" && findTorrentFile(t, task.Path) == nil {
		return true, fmt.Errorf("file not found")
	}
	m.applyDownloadPlan(task.ID, t)
	m.monitorDownloadFile(task.ctx, task.ID, activeDownloadKey(task.ID, task.Path), task.Token, t, task.Path)
	return true, nil
}

func (m *Manager) CancelDownload(ctx context.Context, taskID string) (bool, error) {
	task, ok := m.updateTask(ctx, taskID, func(task *downloadTask) {
		if task.cancel != nil {
			task.cancel()
		}
		task.Status = DownloadTaskStatusCanceled
		task.Active = false
		task.ctx = nil
		task.cancel = nil
	})
	if !ok {
		return false, nil
	}
	return true, m.applyTaskTorrentPlan(ctx, task)
}

func (m *Manager) DeleteDownload(ctx context.Context, taskID string) (bool, error) {
	task, ok := m.deleteTask(taskID)
	if !ok {
		return false, nil
	}
	if task.Active || task.Status == DownloadTaskStatusDownloading {
		m.restoreTask(task)
		return true, fmt.Errorf("download is active")
	}
	if task.cancel != nil {
		task.cancel()
	}
	t, torrentOK, err := m.FileTorrent(ctx, task.ID)
	if torrentOK && err == nil {
		m.applyDownloadPlan(task.ID, t)
		err = m.deleteTaskFiles(t, task)
	}
	if err != nil {
		m.restoreTask(task)
		if torrentOK && t != nil {
			m.applyDownloadPlan(task.ID, t)
		}
		return true, err
	}
	return true, nil
}

func (m *Manager) monitorDownloadFile(ctx context.Context, id, key string, token uint64, t *torrent.Torrent, taskPath string) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if downloadTaskComplete(t, taskPath) {
				m.clearActiveDownload(key, token)
				m.applyDownloadPlan(id, t)
				return
			}
			m.applyDownloadPlan(id, t)
			select {
			case <-ctx.Done():
				m.clearTaskRuntime(key, token)
				m.applyDownloadPlan(id, t)
				return
			case <-ticker.C:
			}
		}
	}()
}

func (m *Manager) applyDownloadPlan(id string, t *torrent.Torrent) {
	tasks := m.activeDownloadTasks(id)
	t.CancelPieces(0, int(t.NumPieces()))
	for _, file := range t.Files() {
		file.SetPriority(torrent.PiecePriorityNone)
	}
	for _, task := range tasks {
		if file := findTorrentFile(t, task.Path); file != nil {
			file.Download()
		}
	}
}

func (m *Manager) activeDownloadTasks(id string) []downloadTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks := make([]downloadTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task.ID == id && task.Active && task.Status == DownloadTaskStatusDownloading {
			tasks = append(tasks, *task)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].Path < tasks[j].Path
	})
	return tasks
}

func (m *Manager) clearActiveDownload(key string, token uint64) {
	m.mu.Lock()
	if current, ok := m.tasks[key]; ok && current.Token == token {
		current.Status = DownloadTaskStatusComplete
		current.Active = false
		current.ctx = nil
		current.cancel = nil
	}
	m.mu.Unlock()
}

func (m *Manager) clearTaskRuntime(key string, token uint64) {
	m.mu.Lock()
	if current, ok := m.tasks[key]; ok && current.Token == token {
		current.Active = false
		current.ctx = nil
		current.cancel = nil
	}
	m.mu.Unlock()
}

func activeDownloadKey(id, filePath string) string {
	return id + "\x00" + filePath
}

func downloadTaskComplete(t *torrent.Torrent, taskPath string) bool {
	file := findTorrentFile(t, taskPath)
	if file == nil {
		return false
	}
	return file.Length() == 0 || file.BytesCompleted() >= file.Length()
}

func (m *Manager) updateTask(_ context.Context, taskID string, update func(*downloadTask)) (downloadTask, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, task, ok := m.findTaskByIDLocked(taskID)
	if !ok {
		return downloadTask{}, false
	}
	update(task)
	return *task, true
}

func (m *Manager) resumeTask(taskID string) (downloadTask, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, task, ok := m.findTaskByIDLocked(taskID)
	if !ok {
		return downloadTask{}, false
	}
	if task.Status == DownloadTaskStatusComplete {
		return *task, true
	}
	if task.Active {
		return *task, true
	}
	m.nextID++
	ctx, cancel := context.WithCancel(m.ctx)
	task.Token = m.nextID
	task.Status = DownloadTaskStatusDownloading
	task.Active = true
	task.ctx = ctx
	task.cancel = cancel
	return *task, true
}

func (m *Manager) deleteTask(taskID string) (downloadTask, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, task, ok := m.findTaskByIDLocked(taskID)
	if !ok {
		return downloadTask{}, false
	}
	out := *task
	delete(m.tasks, key)
	return out, true
}

func (m *Manager) restoreTask(task downloadTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[activeDownloadKey(task.ID, task.Path)] = &task
}

func (m *Manager) findTaskByIDLocked(taskID string) (string, *downloadTask, bool) {
	for key, task := range m.tasks {
		if task.TaskID == taskID {
			return key, task, true
		}
	}
	return "", nil, false
}

func (m *Manager) applyTaskTorrentPlan(ctx context.Context, task downloadTask) error {
	t, ok, err := m.FileTorrent(ctx, task.ID)
	if !ok || err != nil {
		return err
	}
	m.applyDownloadPlan(task.ID, t)
	return nil
}

func (m *Manager) deleteTaskFiles(t *torrent.Torrent, task downloadTask) error {
	file := findTorrentFile(t, task.Path)
	if file == nil {
		return fmt.Errorf("file not found")
	}
	return removeSavedPath(m.saveTo, file.Path())
}

func removeSavedPath(root, filePath string) error {
	fullPath := filepath.Join(root, filepath.FromSlash(filePath))
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return fmt.Errorf("resolve saved path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("refuse to delete outside save directory")
	}
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete saved file: %w", err)
	}
	return nil
}

func (m *Manager) refreshFiles(id string, t *torrent.Torrent) TorrentItem {
	files, _ := m.fileItems(id, t)
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return TorrentItem{}
	}
	if item.Status != TorrentStatusUnavailable && item.Status != TorrentStatusError {
		item.Status = TorrentStatusReady
		item.Error = ""
		item.Files = files
	}
	item.Downloading = m.torrentDownloadingLocked(id)
	return m.cloneItemLocked(item)
}

func (m *Manager) fileItems(id string, t *torrent.Torrent) ([]FileItem, int64) {
	fileTasks := m.downloadTasks(id)
	files := make([]FileItem, 0, len(t.Files()))
	totalBytes := int64(0)
	for _, file := range t.Files() {
		totalBytes += file.Length()
		bytesCompleted := file.BytesCompleted()
		fileTask := fileTasks[file.DisplayPath()]
		task := taskItem(t, fileTask)
		requested := fileTask != nil && fileTask.Status != DownloadTaskStatusCanceled
		active := fileTask != nil && fileTask.Active
		status := fileStatus(file.Length(), bytesCompleted, requested, active)
		completed := int64(0)
		if requested {
			completed = bytesCompleted
		}
		files = append(files, FileItem{
			Path:           file.DisplayPath(),
			Bytes:          file.Length(),
			CompletedBytes: completed,
			SavePath:       filepath.Join(m.saveTo, filepath.FromSlash(file.Path())),
			Status:         status,
			Task:           task,
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, totalBytes
}

func (m *Manager) downloadTasks(id string) map[string]*downloadTask {
	m.mu.Lock()
	defer m.mu.Unlock()

	paths := make(map[string]*downloadTask)
	for _, task := range m.tasks {
		if task.ID != id {
			continue
		}
		paths[task.Path] = cloneDownloadTask(task)
	}
	return paths
}

func cloneDownloadTask(task *downloadTask) *downloadTask {
	if task == nil {
		return nil
	}
	out := *task
	out.ctx = nil
	out.cancel = nil
	return &out
}

func fileStatus(length, bytesCompleted int64, requested, downloading bool) FileStatus {
	switch {
	case requested && (length == 0 || bytesCompleted >= length):
		return FileStatusComplete
	case downloading:
		return FileStatusDownloading
	case requested:
		return FileStatusIdle
	default:
		return FileStatusIdle
	}
}

func taskItem(t *torrent.Torrent, task *downloadTask) *TaskItem {
	if task == nil {
		return nil
	}
	item := &TaskItem{
		ID:        task.TaskID,
		TorrentID: task.ID,
		Status:    task.Status,
	}
	item.Bytes, item.CompletedBytes = downloadTaskProgress(t, *task)
	return item
}

func downloadTaskProgress(t *torrent.Torrent, task downloadTask) (int64, int64) {
	file := findTorrentFile(t, task.Path)
	if file == nil {
		return 0, 0
	}
	return file.Length(), file.BytesCompleted()
}

func findTorrentFile(t *torrent.Torrent, filePath string) *torrent.File {
	for _, file := range t.Files() {
		if file.DisplayPath() == filePath {
			return file
		}
	}
	return nil
}

func isMetadataLoadCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, errManagerClosed)
}

func logMetadataLoadCanceled(ctx context.Context, id string, startedAt time.Time, err error) {
	logrus.WithError(err).WithFields(logging.MergeFields(ctx, logrus.Fields{
		"id":          id,
		"duration_ms": logging.DurationMillis(time.Since(startedAt)),
	})).Debug("get metadata load canceled")
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
		}).Error("get torrent status set to error")
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

func (m *Manager) cloneItemLocked(item *TorrentItem) TorrentItem {
	out := *item
	out.Downloading = m.torrentDownloadingLocked(item.ID)
	if item.Files != nil {
		out.Files = append([]FileItem(nil), item.Files...)
	}
	return out
}

func (m *Manager) torrentDownloadingLocked(id string) bool {
	for _, task := range m.tasks {
		if task.ID == id && task.Active {
			return true
		}
	}
	return false
}
