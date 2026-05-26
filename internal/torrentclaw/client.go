package torrentclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

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

func (c *Client) Name() string {
	return "torrentclaw"
}

type searchResponse struct {
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Results  []searchResult `json:"results"`
}

type searchResult struct {
	ID          int           `json:"id"`
	ContentURL  string        `json:"contentUrl"`
	Title       string        `json:"title"`
	Year        *int          `json:"year"`
	Torrents    []torrentInfo `json:"torrents"`
	HasTorrents bool          `json:"hasTorrents"`
}

type torrentInfo struct {
	InfoHash     string `json:"infoHash"`
	RawTitle     string `json:"rawTitle"`
	Quality      string `json:"quality"`
	Seeders      int    `json:"seeders"`
	Leechers     int    `json:"leechers"`
	MagnetURL    string `json:"magnetUrl"`
	TorrentURL   string `json:"torrentUrl"`
	Source       string `json:"source"`
	QualityScore *int   `json:"qualityScore"`
	UploadedAt   string `json:"uploadedAt"`
}

func (c *Client) Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}

	const pageSize = 50
	pages := (limit + pageSize - 1) / pageSize
	if pages > 4 {
		pages = 4
	}

	var out []model.Torrent
	for page := 1; page <= pages && len(out) < limit; page++ {
		log.WithFields(log.Fields{
			"provider":   c.Name(),
			"query":      req.Query,
			"resolution": req.Resolution,
			"page":       page,
			"limit":      pageSize,
		}).Info("torrentclaw api page request prepared")
		resp, err := c.searchPage(ctx, req.Query, req.Resolution, page, pageSize)
		if err != nil {
			return nil, err
		}

		pageResults := c.flatten(resp)
		log.WithFields(log.Fields{
			"provider": c.Name(),
			"page":     page,
			"contents": len(resp.Results),
			"torrents": len(pageResults),
		}).Info("torrentclaw api page response decoded")
		out = append(out, pageResults...)
		if len(resp.Results) == 0 || len(resp.Results) < resp.PageSize {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func (c *Client) searchPage(ctx context.Context, query, resolution string, page, limit int) (searchResponse, error) {
	u, err := url.Parse(c.apiURL + "/search")
	if err != nil {
		return searchResponse{}, fmt.Errorf("build torrentclaw url: %w", err)
	}

	values := u.Query()
	values.Set("q", query)
	values.Set("quality", resolution)
	values.Set("sort", "seeders")
	values.Set("page", strconv.Itoa(page))
	values.Set("limit", strconv.Itoa(limit))
	u.RawQuery = values.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return searchResponse{}, fmt.Errorf("build torrentclaw request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "mvdl/1.0")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return searchResponse{}, fmt.Errorf("call torrentclaw api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return searchResponse{}, fmt.Errorf("torrentclaw api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return searchResponse{}, fmt.Errorf("decode torrentclaw response: %w", err)
	}

	return out, nil
}

func (c *Client) flatten(resp searchResponse) []model.Torrent {
	var out []model.Torrent
	for _, result := range resp.Results {
		details := absoluteContentURL(result.ContentURL)
		for _, torrent := range result.Torrents {
			hash := torrent.InfoHash
			magnet := torrent.MagnetURL
			link := absoluteTorrentURL(torrent.TorrentURL)

			out = append(out, model.Torrent{
				Provider:     c.Name(),
				Title:        firstNonEmpty(torrent.RawTitle, result.Title),
				Date:         torrent.UploadedAt,
				Details:      details,
				Hash:         stringPtr(hash),
				ID:           hash,
				Link:         stringPtr(link),
				MagnetURL:    stringPtr(magnet),
				Peers:        torrent.Leechers,
				Seeders:      torrent.Seeders,
				Tracker:      torrent.Source,
				TrackerID:    torrent.Source,
				Resolution:   torrent.Quality,
				Source:       torrent.Source,
				QualityScore: torrent.QualityScore,
			})
		}
	}
	return out
}

func absoluteContentURL(path string) string {
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return "https://torrentclaw.com" + path
}

func absoluteTorrentURL(path string) string {
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return "https://torrentclaw.com" + path
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
