package get

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/sirupsen/logrus"

	"4dl/internal/logging"
	"4dl/internal/model"
)

var errManagerClosed = errors.New("get manager closed")
var errSearchResultMissingHash = errors.New("search result missing hash")
var errTaskDeleting = errors.New("task deletion already in progress")
var errTaskNotRunnable = errors.New("task is no longer runnable")

type Manager struct {
	downloadDir string
	engine      *torrentEngine
	store       *taskStore
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	items     map[string]*TaskItem
	downloads map[string]*torrentDownload
	infoBytes map[string][]byte
	deleting  map[string]struct{}
}

type torrentDownload struct {
	ID             string
	Status         TaskStatus
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

type taskDeletion struct {
	item     TaskItem
	runtime  torrentRuntime
	filePath []string
	rootPath string
}

type startedTorrentTask struct {
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	record  StoredTask
}

func NewManager(records []StoredTask, downloadDir, stateDir, torrentListenAddr string, store *taskStore) (*Manager, error) {
	if err := os.MkdirAll(downloadDir, 0o750); err != nil {
		return nil, fmt.Errorf("create download directory: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	engine, err := newTorrentEngine(stateDir, torrentListenAddr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		downloadDir: downloadDir,
		engine:      engine,
		store:       store,
		ctx:         ctx,
		cancel:      cancel,
		items:       make(map[string]*TaskItem, len(records)),
		downloads:   make(map[string]*torrentDownload),
		infoBytes:   make(map[string][]byte, len(records)),
		deleting:    make(map[string]struct{}),
	}
	for i := range records {
		item := records[i].Item
		item.Downloading = false
		if strings.TrimSpace(item.Path) == "" {
			item.Path = downloadDir
		}
		manager.items[item.ID] = &item
		if len(records[i].InfoBytes) > 0 {
			manager.infoBytes[item.ID] = append([]byte(nil), records[i].InfoBytes...)
		}
		if item.Download.Status != "" && item.Download.Status != TaskStatusIdle {
			manager.downloads[item.ID] = &torrentDownload{
				ID:             item.ID,
				Status:         item.Download.Status,
				CompletedBytes: item.Download.CompletedBytes,
				Bytes:          item.Download.Bytes,
			}
		}
	}
	logrus.WithFields(logrus.Fields{
		"items":        len(records),
		"download_dir": downloadDir,
		"state_dir":    stateDir,
	}).Info("get manager initialized")
	manager.restoreStoredTasks()
	return manager, nil
}

func (m *Manager) restoreStoredTasks() {
	type restoreTask struct {
		id     string
		status TaskStatus
	}
	m.mu.Lock()
	tasks := make([]restoreTask, 0, len(m.downloads))
	for id, download := range m.downloads {
		switch download.Status {
		case TaskStatusDownloading, TaskStatusPaused, TaskStatusComplete:
			tasks = append(tasks, restoreTask{id: id, status: download.Status})
		}
	}
	m.mu.Unlock()
	for _, task := range tasks {
		m.restoreStoredTask(task.id, task.status)
	}
}

func (m *Manager) restoreStoredTask(id string, status TaskStatus) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t, ok, err := m.restoreTaskRuntime(m.ctx, id, false)
		if !ok || err != nil {
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, errManagerClosed) {
				logrus.WithError(err).WithField("id", id).Warn("get task restore failed")
			}
			return
		}
		switch status {
		case TaskStatusDownloading:
			m.doStart(id, t)
		case TaskStatusComplete:
			t.AllowDataUpload()
			t.DisallowDataDownload()
			m.attachRestoredTorrent(id, status, false, t)
		case TaskStatusPaused:
			t.DisallowDataUpload()
			t.DisallowDataDownload()
			m.attachRestoredTorrent(id, status, false, t)
		}
		logrus.WithFields(logrus.Fields{
			"id":     id,
			"status": status,
		}).Info("get task restored")
	}()
}

func (m *Manager) attachRestoredTorrent(id string, status TaskStatus, active bool, t *torrent.Torrent) {
	m.mu.Lock()
	if current, ok := m.downloads[id]; ok {
		current.Status = status
		current.Active = active
		current.CompletedBytes = t.BytesCompleted()
		current.Bytes = t.Length()
		current.torrent = t
	}
	if item, ok := m.items[id]; ok {
		item.Downloading = active
		item.Download = m.downloadViewLocked(id, t)
	}
	m.mu.Unlock()
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
	if err := m.engine.Close(); err != nil {
		logrus.WithError(err).Error("torrent engine close failed")
		return err
	}
	if m.store != nil {
		if err := m.store.Close(); err != nil {
			logrus.WithError(err).Error("get task state store close failed")
			return err
		}
	}
	logrus.Info("get manager shut down")
	return nil
}

func (m *Manager) storedRecordLocked(item *TaskItem) StoredTask {
	out := m.cloneItemLocked(item)
	return StoredTask{
		Item:      out,
		InfoBytes: append([]byte(nil), m.infoBytes[item.ID]...),
	}
}

func (m *Manager) saveStoredRecord(record StoredTask) error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(record)
}

func (m *Manager) saveStoredRecordBestEffort(record StoredTask) {
	if err := m.saveStoredRecord(record); err != nil {
		logrus.WithError(err).WithField("id", record.Item.ID).Error("get task state save failed")
	}
}

func (m *Manager) saveItemBestEffort(id string) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	record := m.storedRecordLocked(item)
	m.mu.Unlock()
	m.saveStoredRecordBestEffort(record)
}

func (m *Manager) cloneTaskItem(item TaskItem) TaskItem {
	if item.Files != nil {
		item.Files = append([]FileItem(nil), item.Files...)
	}
	return item
}

func (m *Manager) List() []TaskItem {
	m.mu.Lock()
	out := make([]TaskItem, 0, len(m.items))
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

func (m *Manager) CreateTask(result model.SearchResult, path string) (TaskState, error) {
	path, err := m.resolveTaskPath(path)
	if err != nil {
		return TaskState{}, err
	}
	item, err := taskItemFromSearchResult(result, path)
	if err != nil {
		return TaskState{}, err
	}
	return m.doCreate(item)
}

func (m *Manager) resolveTaskPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = m.downloadDir
	}
	return resolveDownloadDir(path)
}

func (m *Manager) doCreate(item TaskItem) (TaskState, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TaskState{}, errManagerClosed
	}
	if existing, ok := m.items[item.ID]; ok {
		item = m.cloneItemLocked(existing)
		m.mu.Unlock()
		return TaskState{
			TaskItem: item,
			Runtime:  m.runtimeView(item.ID),
		}, nil
	}
	record := StoredTask{
		Item: m.cloneTaskItem(item),
	}
	out := record.Item
	m.mu.Unlock()
	if err := m.saveStoredRecord(record); err != nil {
		return TaskState{}, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		if m.store != nil {
			_ = m.store.Delete(out.ID)
		}
		return TaskState{}, errManagerClosed
	}
	if existing, ok := m.items[out.ID]; ok {
		out = m.cloneItemLocked(existing)
		m.mu.Unlock()
		return TaskState{
			TaskItem: out,
			Runtime:  m.runtimeView(out.ID),
		}, nil
	}
	item = out
	m.items[out.ID] = &item
	m.mu.Unlock()
	return TaskState{
		TaskItem: out,
		Runtime:  m.runtimeView(out.ID),
	}, nil
}

func (m *Manager) Get(id string) (TaskItem, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.items[id]
	if !ok {
		return TaskItem{}, false
	}
	return m.cloneItemLocked(item), true
}

func (m *Manager) State(ctx context.Context) AppState {
	items := m.List()
	state := AppState{
		Updated:     time.Now().Format(time.RFC3339),
		DownloadDir: m.downloadDir,
		Tasks:       make([]TaskState, 0, len(items)),
	}
	for _, item := range items {
		item.Files = nil
		runtime := RuntimeView{Status: RuntimeStatusInactive}
		if item.Downloading || item.Download.Status != TaskStatusIdle {
			runtime = m.runtimeView(item.ID)
		}
		state.Tasks = append(state.Tasks, TaskState{
			TaskItem: item,
			Runtime:  runtime,
		})
	}
	return state
}

func (m *Manager) TaskState(id string) (TaskState, bool) {
	item, ok := m.Get(id)
	if !ok {
		return TaskState{}, false
	}
	item = m.refreshLoadedItem(item)
	return TaskState{
		TaskItem: item,
		Runtime:  m.runtimeView(item.ID),
	}, true
}

func (m *Manager) runtimeView(id string) RuntimeView {
	snapshot, ok, err := m.runtimeSnapshotIfLoaded(id)
	if !ok {
		return RuntimeView{Status: RuntimeStatusError, Error: "task not found"}
	}
	if err != nil {
		return RuntimeView{Status: RuntimeStatusError, Error: err.Error()}
	}
	if snapshot == nil {
		return RuntimeView{Status: RuntimeStatusInactive}
	}
	return RuntimeView{Status: RuntimeStatusReady, Snapshot: snapshot}
}

func (m *Manager) refreshLoadedItem(item TaskItem) TaskItem {
	t, ok, err := m.torrentRuntime(item.ID)
	if ok && err == nil && t != nil && t.Info() != nil {
		return m.refreshFiles(item.ID, t)
	}
	return item
}

func (m *Manager) loadedTorrent(id string) (*torrent.Torrent, bool, error) {
	hash, _, ok := m.itemTorrentSource(id)
	if !ok {
		return nil, false, nil
	}
	return m.engine.loadedTorrent(hash)
}

func (m *Manager) storedInfoBytes(id string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.infoBytes[id]...)
}

func (m *Manager) runtimeSnapshotIfLoaded(id string) (*RuntimeSnapshot, bool, error) {
	t, ok, err := m.torrentRuntime(id)
	if !ok || err != nil {
		return nil, ok, err
	}
	if t == nil {
		return nil, true, nil
	}
	snapshot := m.engine.runtimeSnapshot(id, t)
	return &snapshot, true, nil
}

func (m *Manager) StartTask(ctx context.Context, id string) (TaskItem, bool, error) {
	t, ok, err := m.restoreTaskRuntime(ctx, id, true)
	if !ok || err != nil {
		return TaskItem{}, ok, err
	}
	m.doStart(id, t)
	return m.refreshFiles(id, t), true, nil
}

func (m *Manager) restoreTaskRuntime(ctx context.Context, id string, track bool) (*torrent.Torrent, bool, error) {
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
		})).Warn("get task lookup failed: task not found")
		return nil, false, nil
	}
	hash := item.Hash
	magnetURL := item.MagnetURL
	path := item.Path
	fields := logging.MergeFields(ctx, logrus.Fields{
		"id":         id,
		"title":      item.Title,
		"provider":   item.Provider,
		"hash":       hash,
		"has_magnet": magnetURL != "",
	})
	if track {
		m.wg.Add(1)
	}
	m.mu.Unlock()
	if track {
		defer m.wg.Done()
	}

	logrus.WithFields(fields).Debug("get task metadata wait started")
	t, err := m.engine.addTorrent(hash, magnetURL, path, m.storedInfoBytes(id))
	if err != nil {
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(err).WithFields(fields).Error("get task add failed")
		return nil, true, err
	}
	select {
	case <-ctx.Done():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		entry := logrus.WithError(ctx.Err()).WithFields(fields)
		if errors.Is(ctx.Err(), context.Canceled) {
			entry.Debug("get task metadata wait canceled")
		} else {
			entry.Warn("get task metadata wait canceled")
		}
		return nil, true, ctx.Err()
	case <-t.GotInfo():
		m.cacheMetainfo(id, t)
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		fields["info_hash"] = t.InfoHash().HexString()
		fields["path"] = path
		fields["files"] = len(t.Files())
		logrus.WithFields(fields).Debug("get task metadata ready")
		return t, true, nil
	case <-m.ctx.Done():
		fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
		logrus.WithError(errManagerClosed).WithFields(fields).Debug("get task metadata wait canceled")
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

func (m *Manager) cacheMetainfo(id string, t *torrent.Torrent) {
	infoBytes := append([]byte(nil), t.Metainfo().InfoBytes...)
	m.mu.Lock()
	m.infoBytes[id] = infoBytes
	item, ok := m.items[id]
	var record StoredTask
	if ok {
		record = m.storedRecordLocked(item)
	}
	m.mu.Unlock()
	if ok {
		m.saveStoredRecordBestEffort(record)
	}
}

func (m *Manager) doStart(id string, t *torrent.Torrent) {
	started, ok := m.prepareTorrentStart(id, t)
	if !ok {
		t.CancelPieces(0, int(t.NumPieces()))
		t.DisallowDataUpload()
		t.DisallowDataDownload()
		t.Drop()
		return
	}
	if !started.started {
		return
	}
	m.saveStoredRecordBestEffort(started.record)

	t.AllowDataUpload()
	t.AllowDataDownload()
	t.DownloadAll()
	m.monitorTorrentDownload(started.ctx, id, t)
}

func (m *Manager) prepareTorrentStart(id string, t *torrent.Torrent) (startedTorrentTask, bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return startedTorrentTask{}, false
	}
	if _, ok := m.deleting[id]; ok {
		m.mu.Unlock()
		return startedTorrentTask{}, false
	}
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return startedTorrentTask{}, false
	}
	if current, ok := m.downloads[id]; ok && current.Active {
		m.mu.Unlock()
		return startedTorrentTask{}, true
	}
	ctx, cancel := context.WithCancel(m.ctx)
	if current, ok := m.downloads[id]; ok && current.cancel != nil {
		current.cancel()
	}
	m.downloads[id] = &torrentDownload{
		ID:             id,
		Status:         TaskStatusDownloading,
		Active:         true,
		CompletedBytes: t.BytesCompleted(),
		Bytes:          t.Length(),
		torrent:        t,
		ctx:            ctx,
		cancel:         cancel,
	}
	item.Downloading = true
	item.Download = m.downloadViewLocked(id, t)
	record := m.storedRecordLocked(item)
	m.mu.Unlock()
	return startedTorrentTask{
		started: true,
		ctx:     ctx,
		cancel:  cancel,
		record:  record,
	}, true
}

func (m *Manager) PauseTask(ctx context.Context, id string) (TaskItem, bool, error) {
	return m.doPause(id)
}

func (m *Manager) doPause(id string) (TaskItem, bool, error) {
	runtime, ok, err := m.downloadRuntime(id)
	if !ok || err != nil {
		return TaskItem{}, ok, err
	}
	if runtime.t == nil || !runtime.active {
		item, ok := m.Get(id)
		if !ok {
			return TaskItem{}, false, nil
		}
		return item, true, nil
	}

	t := runtime.t
	t.CancelPieces(0, int(t.NumPieces()))
	t.DisallowDataUpload()
	t.DisallowDataDownload()
	m.mu.Lock()
	var record StoredTask
	recordOK := false
	if current, ok := m.downloads[id]; ok {
		if current.cancel != nil {
			current.cancel()
		}
		current.Status = TaskStatusPaused
		current.Active = false
		current.CompletedBytes = t.BytesCompleted()
		current.Bytes = t.Length()
		current.torrent = t
		current.ctx = nil
		current.cancel = nil
	}
	if item, ok := m.items[id]; ok {
		item.Downloading = false
		item.Download = m.downloadViewLocked(id, t)
		record = m.storedRecordLocked(item)
		recordOK = true
	}
	m.mu.Unlock()
	if recordOK {
		m.saveStoredRecordBestEffort(record)
	}
	return m.refreshFiles(id, t), true, nil
}

func (m *Manager) DeleteTask(ctx context.Context, id string, force bool) (TaskItem, bool, error) {
	deletion, ok, err := m.prepareTaskDeletion(id)
	if !ok || err != nil {
		return TaskItem{}, ok, err
	}
	if err := m.closeTaskRuntime(&deletion); err != nil {
		m.abortTaskDeletion(deletion)
		return TaskItem{}, true, err
	}
	if force {
		if err := m.deleteTaskFiles(deletion); err != nil {
			m.abortTaskDeletion(deletion)
			return TaskItem{}, true, err
		}
	}
	if err := m.commitTaskDeletion(deletion); err != nil {
		m.abortTaskDeletion(deletion)
		return TaskItem{}, true, err
	}
	item := deletion.item
	item.Downloading = false
	item.Download = DownloadView{Status: TaskStatusIdle, Bytes: item.Bytes}
	item.Files = nil
	return item, true, nil
}

func (m *Manager) prepareTaskDeletion(id string) (taskDeletion, bool, error) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return taskDeletion{}, false, nil
	}
	if _, ok := m.deleting[id]; ok {
		m.mu.Unlock()
		return taskDeletion{}, true, errTaskDeleting
	}
	m.deleting[id] = struct{}{}
	deletion := taskDeletion{
		item:     m.cloneItemLocked(item),
		filePath: itemFilePaths(item),
	}
	hash := item.Hash
	if download, ok := m.downloads[id]; ok {
		deletion.runtime = torrentRuntime{t: download.torrent, active: download.Active}
		if download.cancel != nil {
			download.cancel()
		}
		download.Status = TaskStatusPaused
		download.Active = false
		download.ctx = nil
		download.cancel = nil
	}
	item.Downloading = false
	if download, ok := m.downloads[id]; ok {
		item.Download = m.downloadViewLocked(id, download.torrent)
	}
	m.mu.Unlock()

	if deletion.runtime.t == nil && hash != "" {
		t, _, err := m.engine.loadedTorrent(hash)
		if err != nil {
			m.abortTaskDeletion(deletion)
			return taskDeletion{}, true, err
		}
		deletion.runtime.t = t
	}
	if t := deletion.runtime.t; t != nil {
		t.CancelPieces(0, int(t.NumPieces()))
		t.DisallowDataUpload()
		t.DisallowDataDownload()
	}
	return deletion, true, nil
}

func (m *Manager) deleteTaskFiles(deletion taskDeletion) error {
	if deletion.rootPath != "" {
		return removeSavedTree(deletion.rootPath)
	}
	if err := m.deleteFiles(deletion.filePath); err != nil {
		return err
	}
	return pruneEmptyTaskDirs(deletion.item.Path, deletion.filePath)
}

func (m *Manager) closeTaskRuntime(deletion *taskDeletion) error {
	if t := deletion.runtime.t; t != nil {
		if t.Info() != nil {
			paths := m.torrentFilePaths(deletion.item.Path, t)
			m.cacheDeletionFilePaths(deletion.item.ID, paths)
			deletion.rootPath = torrentRootPath(deletion.item.Path, t)
		}
		t.Drop()
	}
	if deletion.rootPath == "" {
		deletion.rootPath = taskRootFromFilePaths(deletion.item.Path, deletion.filePath)
	}
	return nil
}

func (m *Manager) cacheDeletionFilePaths(id string, paths []string) {
	files := make([]FileItem, 0, len(paths)/2)
	for i := 0; i < len(paths); i += 2 {
		files = append(files, FileItem{
			Path:     filepath.Base(paths[i]),
			SavePath: paths[i],
			Status:   FileStatusIdle,
		})
	}
	m.mu.Lock()
	item, ok := m.items[id]
	var record StoredTask
	if ok {
		item.Files = files
		record = m.storedRecordLocked(item)
	}
	m.mu.Unlock()
	if ok {
		m.saveStoredRecordBestEffort(record)
	}
}

func (m *Manager) commitTaskDeletion(deletion taskDeletion) error {
	if m.store != nil {
		if err := m.store.Delete(deletion.item.ID); err != nil {
			return err
		}
	}
	m.mu.Lock()
	delete(m.items, deletion.item.ID)
	delete(m.downloads, deletion.item.ID)
	delete(m.infoBytes, deletion.item.ID)
	delete(m.deleting, deletion.item.ID)
	m.mu.Unlock()
	return nil
}

func (m *Manager) abortTaskDeletion(deletion taskDeletion) {
	m.mu.Lock()
	var record StoredTask
	recordOK := false
	if download, ok := m.downloads[deletion.item.ID]; ok {
		download.Status = TaskStatusPaused
		download.Active = false
		download.torrent = nil
		download.ctx = nil
		download.cancel = nil
	}
	if item, ok := m.items[deletion.item.ID]; ok {
		item.Downloading = false
		item.Download.Status = TaskStatusPaused
		record = m.storedRecordLocked(item)
		recordOK = true
	}
	delete(m.deleting, deletion.item.ID)
	m.mu.Unlock()
	if recordOK {
		m.saveStoredRecordBestEffort(record)
	}
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
	var record StoredTask
	recordOK := false
	if dl, ok := m.downloads[id]; ok && dl.Active {
		dl.CompletedBytes = t.BytesCompleted()
		dl.Bytes = t.Length()
		if item, ok := m.items[id]; ok {
			item.Download = m.downloadViewLocked(id, t)
			record = m.storedRecordLocked(item)
			recordOK = true
		}
	}
	m.mu.Unlock()
	if recordOK {
		m.saveStoredRecordBestEffort(record)
	}
}

func (m *Manager) finishTorrentDownload(id string, t *torrent.Torrent) {
	files, _ := m.fileItems(id, t, false)
	m.mu.Lock()
	var record StoredTask
	recordOK := false
	if current, ok := m.downloads[id]; ok {
		current.Status = TaskStatusComplete
		current.Active = false
		current.CompletedBytes = t.BytesCompleted()
		current.Bytes = t.Length()
		current.torrent = t
		current.ctx = nil
		current.cancel = nil
	}
	if item, ok := m.items[id]; ok {
		item.Error = ""
		item.Files = files
		item.Downloading = false
		item.Download = m.downloadViewLocked(id, t)
		record = m.storedRecordLocked(item)
		recordOK = true
	}
	m.mu.Unlock()
	if recordOK {
		m.saveStoredRecordBestEffort(record)
	}
}

func itemFilePaths(item *TaskItem) []string {
	paths := make([]string, 0, len(item.Files)*2)
	for _, file := range item.Files {
		paths = append(paths, file.SavePath, file.SavePath+".part")
	}
	return paths
}

func (m *Manager) torrentFilePaths(root string, t *torrent.Torrent) []string {
	paths := make([]string, 0, len(t.Files())*2)
	for _, file := range t.Files() {
		path := torrentFileStoragePath(file)
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		paths = append(paths, fullPath, fullPath+".part")
	}
	return paths
}

func torrentRootPath(root string, t *torrent.Torrent) string {
	if t == nil || t.Info() == nil {
		return ""
	}
	name := t.Info().BestName()
	if name == "" || name == metainfo.NoName {
		return ""
	}
	return filepath.Join(root, filepath.FromSlash(name))
}

func taskRootFromFilePaths(root string, paths []string) string {
	if root == "" || len(paths) == 0 {
		return ""
	}
	base, err := savedPath(root)
	if err != nil {
		return ""
	}
	var taskRoot string
	for _, path := range paths {
		fullPath, err := savedPath(path)
		if err != nil {
			return ""
		}
		rel, err := filepath.Rel(base, fullPath)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return ""
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) == 0 || parts[0] == "" || parts[0] == "." {
			return ""
		}
		nextRoot := filepath.Join(base, parts[0])
		if taskRoot == "" {
			taskRoot = nextRoot
			continue
		}
		if taskRoot != nextRoot {
			return ""
		}
	}
	return taskRoot
}

func (m *Manager) deleteFiles(paths []string) error {
	for _, path := range paths {
		if err := removeSavedPath(path); err != nil {
			return err
		}
	}
	return nil
}

func removeSavedTree(path string) error {
	fullPath, err := savedPath(path)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(fullPath); err != nil {
		return fmt.Errorf("delete saved path %q: %w", fullPath, err)
	}
	return nil
}

func removeSavedPath(path string) error {
	fullPath, err := savedPath(path)
	if err != nil {
		return err
	}
	var removeErr error
	for attempt := 0; attempt < 10; attempt++ {
		removeErr = os.Remove(fullPath)
		if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("delete saved file %q: %w", fullPath, removeErr)
}

func pruneEmptyTaskDirs(root string, paths []string) error {
	if root == "" {
		return nil
	}
	base, err := savedPath(root)
	if err != nil {
		return err
	}
	dirs := make(map[string]struct{})
	for _, path := range paths {
		fullPath, err := savedPath(path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(fullPath)
		for {
			rel, err := filepath.Rel(base, dir)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			dirs[dir] = struct{}{}
			dir = filepath.Dir(dir)
		}
	}
	ordered := make([]string, 0, len(dirs))
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return len(ordered[i]) > len(ordered[j])
	})
	for _, dir := range ordered {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
	}
	return nil
}

func savedPath(path string) (string, error) {
	path = filepath.Clean(filepath.FromSlash(path))
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve saved path %q: %w", path, err)
	}
	fullPath = filepath.Clean(fullPath)
	return fullPath, nil
}

func torrentDownloadComplete(t *torrent.Torrent) bool {
	return t.Length() == 0 || t.BytesCompleted() >= t.Length()
}

func (m *Manager) refreshFiles(id string, t *torrent.Torrent) TaskItem {
	downloading := m.torrentDownloading(id)
	files, _ := m.fileItems(id, t, downloading)
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return TaskItem{}
	}
	item.Error = ""
	item.Files = files
	item.Downloading = downloading
	item.Download = m.downloadViewLocked(id, t)
	out := m.cloneItemLocked(item)
	record := m.storedRecordLocked(item)
	m.mu.Unlock()
	m.saveStoredRecordBestEffort(record)
	return out
}

func (m *Manager) fileItems(id string, t *torrent.Torrent, downloading bool) ([]FileItem, int64) {
	files := make([]FileItem, 0, len(t.Files()))
	totalBytes := int64(0)
	root := m.taskPath(id)
	for _, file := range t.Files() {
		totalBytes += file.Length()
		bytesCompleted := file.BytesCompleted()
		path := torrentFileStoragePath(file)
		files = append(files, FileItem{
			Path:           file.DisplayPath(),
			Bytes:          file.Length(),
			CompletedBytes: bytesCompleted,
			SavePath:       filepath.Join(root, filepath.FromSlash(path)),
			Status:         fileStatus(file.Length(), bytesCompleted, downloading),
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, totalBytes
}

func torrentFileStoragePath(file *torrent.File) string {
	return torrentStorageFilePath(file.Torrent().Info(), file.FileInfo())
}

func (m *Manager) taskPath(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item, ok := m.items[id]; ok {
		return item.Path
	}
	return ""
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
	return m.loadedTorrent(id)
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
	t, ok, err := m.loadedTorrent(id)
	return torrentRuntime{t: t}, ok, err
}

func (m *Manager) cloneItemLocked(item *TaskItem) TaskItem {
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
		return DownloadView{Status: TaskStatusIdle}
	}
	if item.Download.Status != "" {
		return item.Download
	}
	return DownloadView{Status: TaskStatusIdle, Bytes: item.Bytes}
}
