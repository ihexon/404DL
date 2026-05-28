package torrentclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"4dl/internal/logging"
	"4dl/internal/magnet"
	"4dl/internal/model"
	"4dl/internal/provider"
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
	Title      string              `json:"title"`
	Candidates []downloadCandidate `json:"torrents"`
}

type downloadCandidate struct {
	InfoHash   string `json:"infoHash"`
	RawTitle   string `json:"rawTitle"`
	SizeBytes  int64  `json:"sizeBytes"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	MagnetURL  string `json:"magnetUrl"`
	UploadedAt string `json:"uploadedAt"`
}

func (c *Client) Search(ctx context.Context, req provider.SearchRequest) ([]model.SearchResult, error) {
	fields := logging.MergeFields(ctx, logrus.Fields{
		"provider": c.Name(),
		"query":    req.Query,
		"sort":     "seeders",
		"auth":     c.apiKey != "",
		"api_url":  c.apiURL,
	})
	logrus.WithFields(fields).Info("torrentclaw api request prepared")

	startedAt := time.Now()
	resp, err := c.searchPage(ctx, req.Query)
	if err != nil {
		return nil, err
	}
	c.logAuthNotice(ctx, resp)

	out := c.flatten(resp)
	fields["raw_groups"] = len(resp.Results)
	fields["normalized_results"] = len(out)
	fields["notice"] = logging.Truncate(resp.Notice, 200)
	fields["duration_ms"] = logging.DurationMillis(time.Since(startedAt))
	fields["with_hash"] = countHashes(out)
	fields["with_magnet"] = countMagnets(out)
	logrus.WithFields(fields).Info("torrentclaw api response decoded")
	return out, nil
}

func (c *Client) logAuthNotice(ctx context.Context, resp searchResponse) {
	if c.apiKey == "" || strings.TrimSpace(resp.Notice) == "" {
		return
	}

	logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
		"provider": c.Name(),
		"notice":   logging.Truncate(resp.Notice, 200),
	})).Warn("torrentclaw api key may not be accepted")
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
	httpReq.Header.Set("User-Agent", "4dl/1.0")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return searchResponse{}, provider.NewRequestError(c.Name(), httpReq, err)
	}
	defer resp.Body.Close()
	logrus.WithFields(logging.MergeFields(ctx, logrus.Fields{
		"provider":    c.Name(),
		"http_method": httpReq.Method,
		"http_url":    httpReq.URL.String(),
		"http_status": resp.StatusCode,
	})).Info("torrentclaw api response received")

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

func (c *Client) flatten(resp searchResponse) []model.SearchResult {
	var out []model.SearchResult
	for _, result := range resp.Results {
		for _, item := range result.Candidates {
			hash := item.InfoHash
			out = append(out, model.SearchResult{
				Provider:  c.Name(),
				Title:     firstNonEmpty(item.RawTitle, result.Title),
				Bytes:     item.SizeBytes,
				Date:      item.UploadedAt,
				Hash:      stringPtr(hash),
				MagnetURL: magnet.NormalizeURLPtr(stringPtr(item.MagnetURL)),
				Peers:     item.Leechers,
				Seeders:   item.Seeders,
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

func countHashes(results []model.SearchResult) int {
	count := 0
	for _, result := range results {
		if result.Hash != nil && strings.TrimSpace(*result.Hash) != "" {
			count++
		}
	}
	return count
}

func countMagnets(results []model.SearchResult) int {
	count := 0
	for _, result := range results {
		if result.MagnetURL != nil && strings.TrimSpace(*result.MagnetURL) != "" {
			count++
		}
	}
	return count
}
