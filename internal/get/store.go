package get

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

const (
	stateDBName        = "state.db"
	taskBucketName     = "tasks"
	stateStoreFileMode = 0o660
	stateAppDirName    = "4dl"
)

var taskBucket = []byte(taskBucketName)

type StoredTask struct {
	Item      TaskItem `json:"item"`
	InfoBytes []byte   `json:"infoBytes,omitempty"`
	Updated   string   `json:"updated"`
}

type taskStore struct {
	db *bbolt.DB
}

func defaultStateDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(base, stateAppDirName), nil
}

func resolveStateDir(stateDir string) (string, error) {
	var err error
	if stateDir == "" {
		stateDir, err = defaultStateDir()
	} else {
		stateDir, err = filepath.Abs(stateDir)
	}
	if err != nil {
		return "", err
	}
	return filepath.Clean(stateDir), nil
}

func resolveDownloadDir(downloadDir string) (string, error) {
	downloadDir = strings.TrimSpace(downloadDir)
	if downloadDir == "" {
		var err error
		downloadDir, err = defaultDownloadDir()
		if err != nil {
			return "", err
		}
	}
	downloadDir, err := filepath.Abs(downloadDir)
	if err != nil {
		return "", fmt.Errorf("resolve download directory %q: %w", downloadDir, err)
	}
	return filepath.Clean(downloadDir), nil
}

func defaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, "Downloads"), nil
}

func openTaskStore(stateDir string) (*taskStore, error) {
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	db, err := bbolt.Open(filepath.Join(stateDir, stateDBName), stateStoreFileMode, &bbolt.Options{
		Timeout: time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open task state db: %w", err)
	}
	store := &taskStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *taskStore) init() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(taskBucket)
		return err
	})
}

func (s *taskStore) Close() error {
	return s.db.Close()
}

func (s *taskStore) Load() ([]StoredTask, error) {
	var out []StoredTask
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(taskBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, value []byte) error {
			var record StoredTask
			if err := json.Unmarshal(value, &record); err != nil {
				return err
			}
			if record.Item.ID == "" {
				return nil
			}
			out = append(out, record)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("load task state: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Item.ID < out[j].Item.ID
	})
	return out, nil
}

func (s *taskStore) Save(record StoredTask) error {
	if record.Item.ID == "" {
		return fmt.Errorf("save task state: missing id")
	}
	record.Updated = time.Now().Format(time.RFC3339)
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal task state %q: %w", record.Item.ID, err)
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(taskBucket)
		if err != nil {
			return err
		}
		return b.Put([]byte(record.Item.ID), data)
	})
}

func (s *taskStore) Delete(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(taskBucket)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(id))
	})
}
