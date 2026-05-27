package httpfs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/sirupsen/logrus"

	"mvdl/internal/crypto"
	"mvdl/internal/magnet"
	"mvdl/internal/model"
)

func loadQueryResults(path, cryptoKey string) ([]TorrentItem, error) {
	startedAt := time.Now()
	reader, closeFn, err := openInput(path)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	var results []model.Torrent
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&results); err != nil {
		logrus.WithError(err).WithField("input", inputLabel(path)).Error("httpfs query input decode failed")
		return nil, fmt.Errorf("decode query JSON: %w", err)
	}

	items := make([]TorrentItem, 0, len(results))
	stats := inputStats{}
	for i, result := range results {
		item := torrentItemFromResult(i, result, cryptoKey, &stats)
		items = append(items, item)
	}
	logrus.WithFields(logrus.Fields{
		"input":              inputLabel(path),
		"records":            len(results),
		"items":              len(items),
		"ready_for_metadata": stats.ready,
		"unavailable":        stats.unavailable,
		"with_hash":          stats.withHash,
		"with_magnet":        stats.withMagnet,
		"encrypted_magnets":  stats.encryptedMagnet,
		"invalid_magnets":    stats.invalidMagnet,
		"duration_ms":        time.Since(startedAt).Milliseconds(),
	}).Info("httpfs query input loaded")
	return items, nil
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		logrus.WithField("input", "stdin").Info("httpfs query input opened")
		return os.Stdin, func() {}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open input file %q: %w", path, err)
	}
	logrus.WithField("input", path).Info("httpfs query input opened")
	return file, func() { _ = file.Close() }, nil
}

type inputStats struct {
	ready           int
	unavailable     int
	withHash        int
	withMagnet      int
	encryptedMagnet int
	invalidMagnet   int
}

func torrentItemFromResult(index int, result model.Torrent, cryptoKey string, stats *inputStats) TorrentItem {
	item := TorrentItem{
		ID:       fmt.Sprintf("%d", index),
		Title:    result.Title,
		Provider: result.Provider,
		Bytes:    result.Bytes,
		Category: result.Category,
		Date:     result.Date,
		Seeders:  result.Seeders,
		Peers:    result.Peers,
		Status:   TorrentStatusIdle,
	}

	magnetURL, encryptedMagnet, magnetErr := resolveMagnet(result.MagnetURL, cryptoKey)
	if encryptedMagnet {
		stats.encryptedMagnet++
	}
	if magnetErr == nil && magnetURL != "" {
		item.MagnetURL = magnetURL
		stats.withMagnet++
		if magnet, err := metainfo.ParseMagnetUri(magnetURL); err == nil {
			item.Hash = magnet.InfoHash.HexString()
			item.ID = item.Hash
			stats.withHash++
		} else {
			stats.invalidMagnet++
			logrus.WithError(err).WithFields(logrus.Fields{
				"index":    index,
				"title":    result.Title,
				"provider": result.Provider,
			}).Warn("httpfs magnet URL parsed without info hash")
		}
		stats.ready++
		return item
	}

	if result.Hash != nil && strings.TrimSpace(*result.Hash) != "" {
		hash := strings.ToLower(strings.TrimSpace(*result.Hash))
		item.Hash = hash
		item.ID = hash
		stats.withHash++
		stats.ready++
		return item
	}

	item.Status = TorrentStatusUnavailable
	stats.unavailable++
	if magnetErr != nil {
		item.Error = magnetErr.Error()
		logrus.WithError(magnetErr).WithFields(logrus.Fields{
			"index":    index,
			"title":    result.Title,
			"provider": result.Provider,
		}).Warn("httpfs torrent item unavailable")
	} else {
		item.Error = "missing hash and magnetUrl"
		logrus.WithFields(logrus.Fields{
			"index":    index,
			"title":    result.Title,
			"provider": result.Provider,
		}).Warn("httpfs torrent item unavailable: missing hash and magnetUrl")
	}
	return item
}

func resolveMagnet(value *string, cryptoKey string) (string, bool, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "", false, nil
	}

	raw := magnet.NormalizeURL(*value)
	if magnet.HasScheme(raw) {
		return raw, false, nil
	}

	if cryptoKey == "" {
		return "", true, fmt.Errorf("magnetUrl is encrypted but MVDL_CRYKEY is not set")
	}
	decryptor, err := crypto.NewStringEncryptor(cryptoKey)
	if err != nil {
		return "", true, fmt.Errorf("invalid MVDL_CRYKEY: %w", err)
	}
	magnetURL, err := decryptor.DecryptString(raw)
	if err != nil {
		return "", true, fmt.Errorf("decrypt magnetUrl: %w", err)
	}
	if !magnet.HasScheme(magnetURL) {
		return "", true, fmt.Errorf("decrypted magnetUrl is not a magnet URL")
	}
	return magnetURL, true, nil
}

func inputLabel(path string) string {
	if path == "" || path == "-" {
		return "stdin"
	}
	return path
}
