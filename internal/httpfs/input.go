package httpfs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/anacrolix/torrent/metainfo"

	"mvdl/internal/crypto"
	"mvdl/internal/magnet"
	"mvdl/internal/model"
)

func loadQueryResults(path, cryptoKey string) ([]TorrentItem, error) {
	reader, closeFn, err := openInput(path)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	var results []model.Torrent
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&results); err != nil {
		return nil, fmt.Errorf("decode query JSON: %w", err)
	}

	items := make([]TorrentItem, 0, len(results))
	for i, result := range results {
		item := torrentItemFromResult(i, result, cryptoKey)
		items = append(items, item)
	}
	return items, nil
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		return os.Stdin, func() {}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open input file %q: %w", path, err)
	}
	return file, func() { _ = file.Close() }, nil
}

func torrentItemFromResult(index int, result model.Torrent, cryptoKey string) TorrentItem {
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

	magnetURL, magnetErr := resolveMagnet(result.MagnetURL, cryptoKey)
	if magnetErr == nil && magnetURL != "" {
		item.MagnetURL = magnetURL
		if magnet, err := metainfo.ParseMagnetUri(magnetURL); err == nil {
			item.Hash = magnet.InfoHash.HexString()
			item.ID = item.Hash
		}
		return item
	}

	if result.Hash != nil && strings.TrimSpace(*result.Hash) != "" {
		hash := strings.ToLower(strings.TrimSpace(*result.Hash))
		item.Hash = hash
		item.ID = hash
		return item
	}

	item.Status = TorrentStatusUnavailable
	if magnetErr != nil {
		item.Error = magnetErr.Error()
	} else {
		item.Error = "missing hash and magnetUrl"
	}
	return item
}

func resolveMagnet(value *string, cryptoKey string) (string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "", nil
	}

	raw := magnet.NormalizeURL(*value)
	if magnet.HasScheme(raw) {
		return raw, nil
	}

	if cryptoKey == "" {
		return "", fmt.Errorf("magnetUrl is encrypted but MVDL_CRYKEY is not set")
	}
	decryptor, err := crypto.NewStringEncryptor(cryptoKey)
	if err != nil {
		return "", fmt.Errorf("invalid MVDL_CRYKEY: %w", err)
	}
	magnetURL, err := decryptor.DecryptString(raw)
	if err != nil {
		return "", fmt.Errorf("decrypt magnetUrl: %w", err)
	}
	if !magnet.HasScheme(magnetURL) {
		return "", fmt.Errorf("decrypted magnetUrl is not a magnet URL")
	}
	return magnetURL, nil
}
