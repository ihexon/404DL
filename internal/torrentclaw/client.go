package torrentclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"

	"mvdl/internal/magnet"
	"mvdl/internal/model"
	"mvdl/internal/provider"
)

const (
	defaultAPIURL = "https://torrentclaw.com/api/v1"
)

type Client struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

type Option func(*Client)

func WithAPIKey(apiKey string) Option {
	return func(c *Client) {
		c.apiKey = strings.TrimSpace(apiKey)
	}
}

func NewClient(apiURL string, httpClient *http.Client, opts ...Option) *Client {
	apiURL = strings.TrimRight(apiURL, "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	client := &Client{
		apiURL:     apiURL,
		httpClient: httpClient,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func (c *Client) Name() string {
	return "torrentclaw"
}

type searchResponse struct {
	Notice  string         `json:"_notice"`
	Results []searchResult `json:"results"`
}

type searchResult struct {
	Title    string        `json:"title"`
	Torrents []torrentInfo `json:"torrents"`
}

type torrentInfo struct {
	InfoHash   string `json:"infoHash"`
	RawTitle   string `json:"rawTitle"`
	SizeBytes  int64  `json:"sizeBytes"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	MagnetURL  string `json:"magnetUrl"`
	UploadedAt string `json:"uploadedAt"`
}

func (c *Client) Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	log.WithFields(log.Fields{
		"provider": c.Name(),
		"query":    req.Query,
		"sort":     "seeders",
		"auth":     c.apiKey != "",
	}).Info("torrentclaw api request prepared")
	resp, err := c.searchPage(ctx, req.Query)
	if err != nil {
		return nil, err
	}
	c.logAuthNotice(resp)

	out := c.flatten(resp)
	log.WithFields(log.Fields{
		"provider": c.Name(),
		"contents": len(resp.Results),
		"torrents": len(out),
		"notice":   resp.Notice,
	}).Info("torrentclaw api response decoded")
	return out, nil
}

func (c *Client) logAuthNotice(resp searchResponse) {
	if c.apiKey == "" || strings.TrimSpace(resp.Notice) == "" {
		return
	}

	log.WithFields(log.Fields{
		"provider": c.Name(),
		"notice":   resp.Notice,
	}).Warn("torrentclaw api key may not be accepted")
}

func (c *Client) searchPage(ctx context.Context, query string) (searchResponse, error) {
	u, err := url.Parse(c.apiURL + "/search")
	if err != nil {
		return searchResponse{}, fmt.Errorf("build torrentclaw url: %w", err)
	}

	values := u.Query()
	values.Set("q", query)
	values.Set("sort", "seeders")
	u.RawQuery = values.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return searchResponse{}, fmt.Errorf("build torrentclaw request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "mvdl/1.0")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return searchResponse{}, provider.NewRequestError(c.Name(), httpReq, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return searchResponse{}, provider.NewStatusError(c.Name(), httpReq, resp.StatusCode, strings.TrimSpace(string(msg)))
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
		for _, torrent := range result.Torrents {
			hash := torrent.InfoHash
			out = append(out, model.Torrent{
				Provider:  c.Name(),
				Title:     firstNonEmpty(torrent.RawTitle, result.Title),
				Bytes:     torrent.SizeBytes,
				Date:      torrent.UploadedAt,
				Hash:      stringPtr(hash),
				MagnetURL: magnet.NormalizeURLPtr(stringPtr(torrent.MagnetURL)),
				Peers:     torrent.Leechers,
				Seeders:   torrent.Seeders,
			})
		}
	}
	return out
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
