package get

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/sirupsen/logrus"

	"4dl/internal/crypto"
	"4dl/internal/magnet"
	"4dl/internal/model"
	"4dl/internal/responsecodec"
)

func loadSearchResults(path, cryptoKey string) ([]TorrentItem, error) {
	startedAt := time.Now()
	reader, closeFn, err := openInput(path)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	input, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read search result input: %w", err)
	}

	results, encrypted, err := decodeSearchResults(input, cryptoKey)
	if err != nil {
		logrus.WithError(err).WithField("input", inputLabel(path)).Error("get search result input decode failed")
		return nil, err
	}

	items := make([]TorrentItem, 0, len(results))
	stats := inputStats{}
	for i, result := range results {
		item := torrentItemFromResult(i, result, &stats)
		items = append(items, item)
	}
	logrus.WithFields(logrus.Fields{
		"input":              inputLabel(path),
		"encrypted_input":    encrypted,
		"records":            len(results),
		"items":              len(items),
		"ready_for_metadata": stats.ready,
		"unavailable":        stats.unavailable,
		"with_hash":          stats.withHash,
		"with_magnet":        stats.withMagnet,
		"invalid_magnets":    stats.invalidMagnet,
		"duration_ms":        time.Since(startedAt).Milliseconds(),
	}).Info("get search result input loaded")
	return items, nil
}

func decodeSearchResults(input []byte, cryptoKey string) ([]model.SearchResult, bool, error) {
	encrypted := cryptoKey != ""
	if encrypted {
		decryptor, err := crypto.NewStringEncryptor(cryptoKey)
		if err != nil {
			return nil, true, fmt.Errorf("invalid FOURDL_CRYKEY: %w", err)
		}
		plaintext, _, err := responsecodec.DecryptBody(input, decryptor)
		if err != nil {
			return nil, true, fmt.Errorf("decrypt search result input: %w", err)
		}
		input = plaintext
	}

	var results []model.SearchResult
	if err := json.Unmarshal(input, &results); err != nil {
		return nil, encrypted, fmt.Errorf("decode search result JSON: %w", err)
	}
	return results, encrypted, nil
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		logrus.WithField("input", "stdin").Info("get search result input opened")
		return os.Stdin, func() {}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open input file %q: %w", path, err)
	}
	logrus.WithField("input", path).Info("get search result input opened")
	return file, func() { _ = file.Close() }, nil
}

type inputStats struct {
	ready         int
	unavailable   int
	withHash      int
	withMagnet    int
	invalidMagnet int
}

func torrentItemFromResult(index int, result model.SearchResult, stats *inputStats) TorrentItem {
	item := TorrentItem{
		ID:       fmt.Sprintf("%d", index),
		Title:    result.Title,
		Provider: result.Provider,
		Bytes:    result.Bytes,
		Category: result.Category,
		Date:     result.Date,
		Seeders:  result.Seeders,
		Peers:    result.Peers,
	}

	magnetURL := resolveMagnet(result.MagnetURL)
	if magnetURL != "" {
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
			}).Warn("get magnet URL parsed without info hash")
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

	stats.unavailable++
	item.Error = "missing hash and magnetUrl"
	logrus.WithFields(logrus.Fields{
		"index":    index,
		"title":    result.Title,
		"provider": result.Provider,
	}).Warn("get torrent item unavailable: missing hash and magnetUrl")
	return item
}

func resolveMagnet(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return ""
	}

	raw := magnet.NormalizeURL(*value)
	if magnet.HasScheme(raw) {
		return raw
	}
	return ""
}

func inputLabel(path string) string {
	if path == "" || path == "-" {
		return "stdin"
	}
	return path
}
