package get

import (
	"strings"

	"4dl/internal/magnet"
	"4dl/internal/model"
)

func taskItemFromSearchResult(result model.SearchResult, path string) (TaskItem, error) {
	hash := normalizeSearchResultHash(result.Hash)
	if hash == "" {
		return TaskItem{}, errSearchResultMissingHash
	}

	return TaskItem{
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
		Path:      path,
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
