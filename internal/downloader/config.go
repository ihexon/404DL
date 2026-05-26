package downloader

import (
	"io"
	"time"
)

type Config struct {
	DataDir          string
	ProgressInterval time.Duration
	ProgressWriter   io.Writer
	StatusAddr       string
	CloseTimeout     time.Duration
	HTTPTimeout      time.Duration

	EstablishedConnsPerTorrent int
	HalfOpenConnsPerTorrent    int
	TotalHalfOpenConns         int
	TorrentPeersHighWater      int
	TorrentPeersLowWater       int
	MaxUnverifiedBytes         int64
	MaxPeerRequestBufferBytes  int
	DialRateLimit              float64
	DialRateBurst              int
	PieceHashersPerTorrent     int
	NoUpload                   bool
	Seed                       bool
	DisableIPv6                bool
	ListenAddr                 string
	DownloadRateBytesPerSec    int
	UploadRateBytesPerSec      int
}
