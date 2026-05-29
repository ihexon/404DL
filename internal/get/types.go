package get

import (
	"context"

	"4dl/internal/model"
	"4dl/internal/provider"
)

type Config struct {
	ListenAddr        string
	SaveTo            string
	TorrentListenAddr string
	Searcher          Searcher
	DefaultLimit      int
}

type HealthResponse struct {
	Status string `json:"status"`
}

type Searcher interface {
	Search(context.Context, provider.SearchRequest) ([]model.SearchResult, error)
}

type SearchRequest struct {
	Query     string   `json:"query"`
	Limit     int      `json:"limit,omitempty"`
	Providers []string `json:"providers,omitempty"`
}

type CreateDownloadRequest struct {
	Result model.SearchResult `json:"result"`
}

type FileStatus string

const (
	FileStatusIdle        FileStatus = "idle"
	FileStatusDownloading FileStatus = "downloading"
	FileStatusComplete    FileStatus = "complete"
)

type TorrentDownloadStatus string

const (
	TorrentDownloadStatusIdle        TorrentDownloadStatus = "idle"
	TorrentDownloadStatusDownloading TorrentDownloadStatus = "downloading"
	TorrentDownloadStatusPaused      TorrentDownloadStatus = "paused"
	TorrentDownloadStatusComplete    TorrentDownloadStatus = "complete"
)

type TorrentItem struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Provider    string       `json:"provider"`
	Bytes       int64        `json:"bytes,omitempty"`
	Category    string       `json:"category,omitempty"`
	Date        string       `json:"date,omitempty"`
	Seeders     int          `json:"seeders"`
	Peers       int          `json:"peers"`
	Hash        string       `json:"hash,omitempty"`
	MagnetURL   string       `json:"magnetUrl,omitempty"`
	Downloading bool         `json:"downloading"`
	Download    DownloadView `json:"download"`
	Error       string       `json:"error,omitempty"`
	Files       []FileItem   `json:"files,omitempty"`
}

type DownloadView struct {
	Status         TorrentDownloadStatus `json:"status"`
	CompletedBytes int64                 `json:"completedBytes"`
	Bytes          int64                 `json:"bytes"`
}

type AppState struct {
	Updated       string               `json:"updated"`
	SaveTo        string               `json:"saveTo"`
	SearchResults []model.SearchResult `json:"searchResults"`
	Torrents      []TorrentState       `json:"torrents"`
}

type TorrentState struct {
	TorrentItem
	Runtime RuntimeView `json:"runtime"`
}

type RuntimeStatus string

const (
	RuntimeStatusInactive RuntimeStatus = "inactive"
	RuntimeStatusReady    RuntimeStatus = "ready"
	RuntimeStatusError    RuntimeStatus = "error"
)

type RuntimeView struct {
	Status   RuntimeStatus    `json:"status"`
	Snapshot *RuntimeSnapshot `json:"snapshot,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type FileItem struct {
	Path           string     `json:"path"`
	Bytes          int64      `json:"bytes"`
	CompletedBytes int64      `json:"completedBytes"`
	SavePath       string     `json:"savePath"`
	Status         FileStatus `json:"status"`
}

type APIError struct {
	Error string `json:"error"`
}
