package magnet

import (
	"net/url"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

const scheme = "magnet:"

func NormalizeURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || HasScheme(value) {
		return value
	}

	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return value
	}
	if !HasScheme(decoded) {
		return value
	}
	return decoded
}

func NormalizeURLPtr(value *string) *string {
	if value == nil {
		return nil
	}

	normalized := NormalizeURL(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func HasScheme(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), scheme)
}

func InfoHash(value string) string {
	parsed, err := metainfo.ParseMagnetUri(NormalizeURL(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.InfoHash.HexString())
}

func DisplayName(value string) string {
	parsed, err := metainfo.ParseMagnetUri(NormalizeURL(value))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.DisplayName)
}
