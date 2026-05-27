package httpfs

import "mvdl/internal/model"

type Config struct {
	ListenAddr        string
	InputPath         string
	DataDir           string
	TorrentListenAddr string
	CryptoKey         string
}

type TorrentStatus string

const (
	TorrentStatusUnavailable TorrentStatus = "unavailable"
	TorrentStatusIdle        TorrentStatus = "idle"
	TorrentStatusLoading     TorrentStatus = "loading"
	TorrentStatusReady       TorrentStatus = "ready"
	TorrentStatusError       TorrentStatus = "error"
)

type TorrentItem struct {
	ID           string        `json:"id"`
	Source       model.Torrent `json:"source"`
	Hash         string        `json:"hash,omitempty"`
	MagnetURL    string        `json:"magnetUrl,omitempty"`
	Status       TorrentStatus `json:"status"`
	Error        string        `json:"error,omitempty"`
	Files        []FileItem    `json:"files,omitempty"`
	DownloadBase string        `json:"downloadBase,omitempty"`
}

type FileItem struct {
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	DownloadURL string `json:"downloadUrl"`
}

type APIError struct {
	Error string `json:"error"`
}
