package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type TMDBClient struct {
	apiURL     string
	apiKey     string
	bearer     bool
	httpClient *http.Client
}

type TMDBOptions struct {
	APIURL     string
	APIKey     string
	HTTPClient *http.Client
}

func NewTMDBClient(opts TMDBOptions) *TMDBClient {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	apiURL := strings.TrimRight(opts.APIURL, "/")
	if apiURL == "" {
		apiURL = "https://api.themoviedb.org/3"
	}

	return &TMDBClient{
		apiURL:     apiURL,
		apiKey:     opts.APIKey,
		bearer:     strings.HasPrefix(opts.APIKey, "eyJ"),
		httpClient: httpClient,
	}
}

type tmdbSearchResponse struct {
	Results []tmdbMovie `json:"results"`
}

type tmdbMovie struct {
	Title           string  `json:"title"`
	OriginalTitle   string  `json:"original_title"`
	OriginalLang    string  `json:"original_language"`
	ReleaseDate     string  `json:"release_date"`
	Popularity      float64 `json:"popularity"`
	VoteCount       int     `json:"vote_count"`
	Adult           bool    `json:"adult"`
	BackdropPath    string  `json:"backdrop_path"`
	OriginalName    string  `json:"original_name"`
	PosterPath      string  `json:"poster_path"`
	TranslatedTitle string  `json:"name"`
}

func (c *TMDBClient) ResolveMovie(ctx context.Context, query string) (Movie, error) {
	query = strings.TrimSpace(query)
	if query == "" || c.apiKey == "" {
		return Movie{}, nil
	}

	resp, err := c.search(ctx, query)
	if err != nil {
		return Movie{}, err
	}
	if len(resp.Results) == 0 {
		return Movie{}, nil
	}

	best := pickBestMovie(resp.Results)
	title := best.OriginalTitle
	if title == "" {
		title = best.Title
	}

	return Movie{
		Title: normalizeTitle(title),
		Year:  yearFromDate(best.ReleaseDate),
	}, nil
}

func (c *TMDBClient) search(ctx context.Context, query string) (tmdbSearchResponse, error) {
	u, err := url.Parse(c.apiURL + "/search/movie")
	if err != nil {
		return tmdbSearchResponse{}, fmt.Errorf("build tmdb url: %w", err)
	}

	values := u.Query()
	values.Set("query", query)
	values.Set("language", "zh-CN")
	values.Set("include_adult", "false")
	values.Set("page", "1")
	if !c.bearer {
		values.Set("api_key", c.apiKey)
	}
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tmdbSearchResponse{}, fmt.Errorf("build tmdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mvdl/1.0")
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return tmdbSearchResponse{}, fmt.Errorf("call tmdb api: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return tmdbSearchResponse{}, fmt.Errorf("tmdb api returned %d: %s", httpResp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out tmdbSearchResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&out); err != nil {
		return tmdbSearchResponse{}, fmt.Errorf("decode tmdb response: %w", err)
	}
	return out, nil
}

func pickBestMovie(movies []tmdbMovie) tmdbMovie {
	best := movies[0]
	bestScore := movieScore(best)
	for _, movie := range movies[1:] {
		score := movieScore(movie)
		if score > bestScore {
			best = movie
			bestScore = score
		}
	}
	return best
}

func movieScore(movie tmdbMovie) float64 {
	score := movie.Popularity
	if movie.ReleaseDate != "" {
		score += 10
	}
	if movie.OriginalLang == "en" {
		score += 5
	}
	if movie.VoteCount > 0 {
		score += float64(min(movie.VoteCount, 1000)) / 100
	}
	return score
}

func yearFromDate(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}

func normalizeTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, ":", " ")
	title = strings.Join(strings.Fields(title), " ")
	return title
}
