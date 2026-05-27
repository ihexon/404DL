package get

type Config struct {
	ListenAddr        string
	InputPath         string
	SaveTo            string
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

type FileStatus string

const (
	FileStatusIdle        FileStatus = "idle"
	FileStatusDownloading FileStatus = "downloading"
	FileStatusComplete    FileStatus = "complete"
)

type DownloadTaskStatus string

const (
	DownloadTaskStatusDownloading DownloadTaskStatus = "downloading"
	DownloadTaskStatusPaused      DownloadTaskStatus = "paused"
	DownloadTaskStatusComplete    DownloadTaskStatus = "complete"
	DownloadTaskStatusCanceled    DownloadTaskStatus = "canceled"
)

type TorrentItem struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Provider    string        `json:"provider"`
	Bytes       int64         `json:"bytes,omitempty"`
	Category    string        `json:"category,omitempty"`
	Date        string        `json:"date,omitempty"`
	Seeders     int           `json:"seeders"`
	Peers       int           `json:"peers"`
	Hash        string        `json:"hash,omitempty"`
	MagnetURL   string        `json:"magnetUrl,omitempty"`
	Status      TorrentStatus `json:"status"`
	Downloading bool          `json:"downloading"`
	Error       string        `json:"error,omitempty"`
	Files       []FileItem    `json:"files,omitempty"`
}

type AppState struct {
	Updated  string         `json:"updated"`
	SaveTo   string         `json:"saveTo"`
	Torrents []TorrentState `json:"torrents"`
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
	Task           *TaskItem  `json:"task,omitempty"`
}

type TaskItem struct {
	ID             string             `json:"id"`
	TorrentID      string             `json:"torrentId"`
	Status         DownloadTaskStatus `json:"status"`
	CompletedBytes int64              `json:"completedBytes"`
	Bytes          int64              `json:"bytes"`
}

type APIError struct {
	Error string `json:"error"`
}

type FileDownloadRequest struct {
	Path string `json:"path"`
}
