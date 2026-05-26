package downloader

import (
	"fmt"
	"io"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/dustin/go-humanize"
)

type progress struct {
	CompletedBytes int64
	TotalBytes     int64
	DownloadRate   int64
}

type progressReporter struct {
	t         *torrent.Torrent
	lastAt    time.Time
	lastStats torrent.TorrentStats
}

func newProgressReporter(t *torrent.Torrent) *progressReporter {
	return &progressReporter{
		t:         t,
		lastAt:    time.Now(),
		lastStats: t.Stats(),
	}
}

func (r *progressReporter) progress() progress {
	now := time.Now()
	stats := r.t.Stats()
	interval := now.Sub(r.lastAt)
	if interval <= 0 {
		interval = time.Second
	}

	rate := bytesPerSecond(
		stats.BytesReadUsefulData.Int64()-r.lastStats.BytesReadUsefulData.Int64(),
		interval,
	)
	r.lastAt = now
	r.lastStats = stats

	return progress{
		CompletedBytes: r.t.BytesCompleted(),
		TotalBytes:     r.t.Info().TotalLength(),
		DownloadRate:   rate,
	}
}

func bytesPerSecond(bytes int64, interval time.Duration) int64 {
	return bytes * int64(time.Second) / int64(interval)
}

func writeProgress(w io.Writer, p progress) {
	if w == nil {
		return
	}

	fmt.Fprintf(
		w,
		"total: %s, downloaded: %s, speed: %s/s\n",
		humanize.Bytes(uint64(p.TotalBytes)),
		humanize.Bytes(uint64(p.CompletedBytes)),
		humanize.Bytes(uint64(p.DownloadRate)),
	)
}
