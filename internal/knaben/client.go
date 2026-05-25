package knaben

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type Client struct {
	apiURL     string
	httpClient *http.Client
}

func NewClient(apiURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		apiURL:     strings.TrimRight(apiURL, "/"),
		httpClient: httpClient,
	}
}

type SearchRequest struct {
	SearchType     string `json:"search_type"`
	SearchField    string `json:"search_field"`
	Query          string `json:"query"`
	OrderBy        string `json:"order_by"`
	OrderDirection string `json:"order_direction"`
	From           int    `json:"from"`
	Size           int    `json:"size"`
	HideUnsafe     bool   `json:"hide_unsafe"`
	HideXXX        bool   `json:"hide_xxx"`
}

type SearchResponse struct {
	Hits []torrent `json:"hits"`
}

type torrent struct {
	Bytes          int64    `json:"bytes"`
	CachedOrigin   string   `json:"cachedOrigin,omitempty"`
	Category       string   `json:"category,omitempty"`
	CategoryID     []int    `json:"categoryId,omitempty"`
	Date           string   `json:"date,omitempty"`
	Details        string   `json:"details,omitempty"`
	Hash           *string  `json:"hash"`
	ID             string   `json:"id,omitempty"`
	LastSeen       string   `json:"lastSeen,omitempty"`
	Link           *string  `json:"link,omitempty"`
	MagnetURL      *string  `json:"magnetUrl,omitempty"`
	Peers          int      `json:"peers"`
	Score          *float64 `json:"score"`
	Seeders        int      `json:"seeders"`
	Title          string   `json:"title"`
	Tracker        string   `json:"tracker,omitempty"`
	TrackerID      string   `json:"trackerId,omitempty"`
	VirusDetection float64  `json:"virusDetection,omitempty"`
}

func (c *Client) Name() string {
	return "knaben"
}

func (c *Client) Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	size := req.Limit
	if size <= 0 || size > 200 {
		size = 200
	}

	hits, err := c.search(ctx, SearchRequest{
		SearchType:     "100%",
		SearchField:    "title",
		Query:          req.Query,
		OrderBy:        "seeders",
		OrderDirection: "desc",
		From:           0,
		Size:           size,
		HideUnsafe:     true,
		HideXXX:        true,
	})
	if err != nil {
		return nil, err
	}

	out := make([]model.Torrent, 0, len(hits))
	for _, hit := range hits {
		out = append(out, model.Torrent{
			Provider:       c.Name(),
			Title:          hit.Title,
			Bytes:          hit.Bytes,
			Category:       hit.Category,
			Date:           hit.Date,
			Details:        hit.Details,
			Hash:           hit.Hash,
			ID:             hit.ID,
			LastSeen:       hit.LastSeen,
			Link:           hit.Link,
			MagnetURL:      hit.MagnetURL,
			Peers:          hit.Peers,
			Seeders:        hit.Seeders,
			Tracker:        hit.Tracker,
			TrackerID:      hit.TrackerID,
			VirusDetection: &hit.VirusDetection,
		})
	}

	return out, nil
}

func (c *Client) search(ctx context.Context, req SearchRequest) ([]torrent, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal knaben request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build knaben request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "mvdl/1.0")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call knaben api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("knaben api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode knaben response: %w", err)
	}
	if out.Hits == nil {
		out.Hits = []torrent{}
	}

	return out.Hits, nil
}
