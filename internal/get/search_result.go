package get

import (
	"strings"

	"4dl/internal/magnet"
	"4dl/internal/model"
)

func torrentItemFromSearchResult(result model.SearchResult) (TorrentItem, error) {
	hash := normalizeSearchResultHash(result.Hash)
	if hash == "" {
		return TorrentItem{}, errSearchResultMissingHash
	}

	return TorrentItem{
		ID:        hash,
		Title:     result.Title,
		Provider:  result.Provider,
		Bytes:     result.Bytes,
		Category:  result.Category,
		Date:      result.Date,
		Seeders:   result.Seeders,
		Peers:     result.Peers,
		Hash:      hash,
		MagnetURL: normalizeSearchResultMagnet(result.MagnetURL),
	}, nil
}

func normalizeSearchResultHash(value *string) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*value))
}

func normalizeSearchResultMagnet(value *string) string {
	if value == nil {
		return ""
	}
	normalized := magnet.NormalizeURL(*value)
	if !magnet.HasScheme(normalized) {
		return ""
	}
	return normalized
}
