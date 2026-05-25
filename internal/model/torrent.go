package model

type Torrent struct {
	Provider       string   `json:"provider"`
	Title          string   `json:"title"`
	Bytes          int64    `json:"bytes,omitempty"`
	Category       string   `json:"category,omitempty"`
	Date           string   `json:"date,omitempty"`
	Details        string   `json:"details,omitempty"`
	Hash           *string  `json:"hash"`
	ID             string   `json:"id,omitempty"`
	LastSeen       string   `json:"lastSeen,omitempty"`
	Link           *string  `json:"link,omitempty"`
	MagnetURL      *string  `json:"magnetUrl,omitempty"`
	Peers          int      `json:"peers"`
	Seeders        int      `json:"seeders"`
	Tracker        string   `json:"tracker,omitempty"`
	TrackerID      string   `json:"trackerId,omitempty"`
	Resolution     string   `json:"resolution,omitempty"`
	Source         string   `json:"source,omitempty"`
	QualityScore   *int     `json:"qualityScore,omitempty"`
	VirusDetection *float64 `json:"virusDetection,omitempty"`
}
