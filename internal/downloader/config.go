package downloader

import (
	"fmt"
	"io"
	"strings"
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

func (c Config) Validate() error {
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("download data directory is required")
	}
	if c.ProgressInterval < 0 {
		return fmt.Errorf("progress interval must be non-negative")
	}
	if c.CloseTimeout < 0 {
		return fmt.Errorf("close timeout must be non-negative")
	}
	if c.HTTPTimeout < 0 {
		return fmt.Errorf("http timeout must be non-negative")
	}
	if c.DownloadRateBytesPerSec < 0 {
		return fmt.Errorf("download rate limit must be non-negative")
	}
	if c.UploadRateBytesPerSec < 0 {
		return fmt.Errorf("upload rate limit must be non-negative")
	}
	if c.MaxUnverifiedBytes < 0 {
		return fmt.Errorf("maximum unverified bytes must be non-negative")
	}
	return nil
}
