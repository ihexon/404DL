package model

type SearchResult struct {
	Provider  string  `json:"provider"`
	Title     string  `json:"title"`
	Bytes     int64   `json:"bytes,omitempty"`
	Category  string  `json:"category,omitempty"`
	Date      string  `json:"date,omitempty"`
	Seeders   int     `json:"seeders"`
	Peers     int     `json:"peers"`
	Hash      *string `json:"hash,omitempty"`
	MagnetURL *string `json:"magnetUrl,omitempty"`
}
