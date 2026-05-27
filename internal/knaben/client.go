package knaben

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"

	"mvdl/internal/magnet"
	"mvdl/internal/model"
	"mvdl/internal/provider"
)

const (
	defaultAPIURL = "https://api.knaben.org/v1"
)

type Client struct {
	apiURL     string
	httpClient *http.Client
}

func NewClient(apiURL string, httpClient *http.Client) *Client {
	apiURL = strings.TrimRight(apiURL, "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		apiURL:     apiURL,
		httpClient: httpClient,
	}
}

type SearchRequest struct {
	SearchType     string `json:"search_type"`
	SearchField    string `json:"search_field"`
	Query          string `json:"query"`
	OrderBy        string `json:"order_by"`
	OrderDirection string `json:"order_direction"`
	HideUnsafe     bool   `json:"hide_unsafe"`
	HideXXX        bool   `json:"hide_xxx"`
}

type SearchResponse struct {
	Hits []torrent `json:"hits"`
}

type torrent struct {
	Bytes     int64   `json:"bytes"`
	Category  string  `json:"category,omitempty"`
	Date      string  `json:"date,omitempty"`
	Hash      *string `json:"hash"`
	MagnetURL *string `json:"magnetUrl,omitempty"`
	Peers     int     `json:"peers"`
	Seeders   int     `json:"seeders"`
	Title     string  `json:"title"`
}

func (c *Client) Name() string {
	return "knaben"
}

func (c *Client) Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	log.WithFields(log.Fields{
		"provider": c.Name(),
		"query":    req.Query,
		"sort":     "seeders desc",
	}).Info("knaben api request prepared")

	hits, err := c.search(ctx, SearchRequest{
		SearchType:     "100%",
		SearchField:    "title",
		Query:          req.Query,
		OrderBy:        "seeders",
		OrderDirection: "desc",
		HideUnsafe:     true,
		HideXXX:        true,
	})
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{
		"provider": c.Name(),
		"count":    len(hits),
	}).Info("knaben api response decoded")

	out := make([]model.Torrent, 0, len(hits))
	for _, hit := range hits {
		out = append(out, model.Torrent{
			Provider:  c.Name(),
			Title:     hit.Title,
			Bytes:     hit.Bytes,
			Category:  hit.Category,
			Date:      hit.Date,
			Hash:      hit.Hash,
			MagnetURL: magnet.NormalizeURLPtr(hit.MagnetURL),
			Peers:     hit.Peers,
			Seeders:   hit.Seeders,
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
		return nil, provider.NewRequestError(c.Name(), httpReq, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, provider.NewStatusError(c.Name(), httpReq, resp.StatusCode, strings.TrimSpace(string(msg)))
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
