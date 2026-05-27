package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"mvdl/internal/provider"
)

type TMDBClient struct {
	apiURL     string
	apiKey     string
	bearer     bool
	language   string
	httpClient *http.Client
}

type TMDBOptions struct {
	APIURL     string
	APIKey     string
	Language   string
	HTTPClient *http.Client
}

type TMDBMediaType string

const (
	TMDBMediaMovie TMDBMediaType = "movie"
	TMDBMediaTV    TMDBMediaType = "tv"
)

func NewTMDBClient(opts TMDBOptions) *TMDBClient {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	apiURL := strings.TrimRight(opts.APIURL, "/")
	if apiURL == "" {
		apiURL = "https://api.themoviedb.org/3"
	}
	language := strings.TrimSpace(opts.Language)
	if language == "" {
		language = "en-US"
	}

	return &TMDBClient{
		apiURL:     apiURL,
		apiKey:     opts.APIKey,
		bearer:     strings.HasPrefix(opts.APIKey, "eyJ"),
		language:   language,
		httpClient: httpClient,
	}
}

type TMDBSearchResponse struct {
	Page         int              `json:"page,omitempty"`
	Results      []map[string]any `json:"results"`
	TotalPages   int              `json:"total_pages,omitempty"`
	TotalResults int              `json:"total_results,omitempty"`
}

type TMDBFormattedResponse struct {
	TotalResults int                   `json:"totalResults"`
	Results      []TMDBFormattedResult `json:"results"`
}

type TMDBFormattedResult struct {
	ID               int                   `json:"id"`
	Title            string                `json:"title,omitempty"`
	OriginalTitle    string                `json:"originalTitle,omitempty"`
	Year             int                   `json:"year,omitempty"`
	FirstAirDate     string                `json:"firstAirDate,omitempty"`
	ReleaseDate      string                `json:"releaseDate,omitempty"`
	Status           string                `json:"status,omitempty"`
	Type             string                `json:"type,omitempty"`
	Overview         string                `json:"overview,omitempty"`
	VoteAverage      float64               `json:"voteAverage,omitempty"`
	VoteCount        int                   `json:"voteCount,omitempty"`
	Popularity       float64               `json:"popularity,omitempty"`
	NumberOfSeasons  int                   `json:"numberOfSeasons,omitempty"`
	NumberOfEpisodes int                   `json:"numberOfEpisodes,omitempty"`
	LastEpisode      *TMDBFormattedEpisode `json:"lastEpisode,omitempty"`
	NextEpisode      *TMDBFormattedEpisode `json:"nextEpisode,omitempty"`
	Seasons          []TMDBFormattedSeason `json:"seasons,omitempty"`
}

type TMDBFormattedEpisode struct {
	Season  int    `json:"season,omitempty"`
	Episode int    `json:"episode,omitempty"`
	Name    string `json:"name,omitempty"`
	AirDate string `json:"airDate,omitempty"`
	Runtime int    `json:"runtime,omitempty"`
}

type TMDBFormattedSeason struct {
	Season       int                    `json:"season"`
	Name         string                 `json:"name,omitempty"`
	Episodes     int                    `json:"episodes"`
	AirDate      string                 `json:"airDate,omitempty"`
	VoteAverage  float64                `json:"voteAverage,omitempty"`
	Specials     bool                   `json:"specials,omitempty"`
	RegularOrder int                    `json:"regularOrder,omitempty"`
	EpisodeList  []TMDBFormattedEpisode `json:"episodeList,omitempty"`
}

func (c *TMDBClient) Search(ctx context.Context, mediaType TMDBMediaType, query string) (TMDBSearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return TMDBSearchResponse{}, nil
	}
	if mediaType != TMDBMediaMovie && mediaType != TMDBMediaTV {
		return TMDBSearchResponse{}, fmt.Errorf("unsupported tmdb media type %q", mediaType)
	}
	if c.apiKey == "" {
		return TMDBSearchResponse{}, fmt.Errorf("%s is required", "tmdb api key")
	}

	resp, err := c.searchPath(ctx, "/search/"+string(mediaType), query)
	if err != nil {
		return TMDBSearchResponse{}, err
	}
	sort.SliceStable(resp.Results, func(i, j int) bool {
		return tmdbLess(mediaType, resp.Results[i], resp.Results[j])
	})
	return resp, nil
}

func FormatTMDBSearchResponse(resp TMDBSearchResponse) TMDBFormattedResponse {
	out := TMDBFormattedResponse{
		TotalResults: resp.TotalResults,
		Results:      make([]TMDBFormattedResult, 0, len(resp.Results)),
	}
	for _, result := range resp.Results {
		out.Results = append(out.Results, formatTMDBResult(result))
	}
	return out
}

func formatTMDBResult(result map[string]any) TMDBFormattedResult {
	return TMDBFormattedResult{
		ID:               tmdbInt(result, "id"),
		Title:            tmdbTitle(result),
		OriginalTitle:    tmdbOriginalTitle(result),
		Year:             tmdbYear(result),
		FirstAirDate:     tmdbString(result, "first_air_date"),
		ReleaseDate:      tmdbString(result, "release_date"),
		Status:           tmdbString(result, "status"),
		Type:             tmdbString(result, "type"),
		Overview:         tmdbString(result, "overview"),
		VoteAverage:      tmdbFloat(result, "vote_average"),
		VoteCount:        tmdbInt(result, "vote_count"),
		Popularity:       tmdbFloat(result, "popularity"),
		NumberOfSeasons:  tmdbInt(result, "number_of_seasons"),
		NumberOfEpisodes: tmdbInt(result, "number_of_episodes"),
		LastEpisode:      formatTMDBEpisode(tmdbMap(result, "last_episode_to_air")),
		NextEpisode:      formatTMDBEpisode(tmdbMap(result, "next_episode_to_air")),
		Seasons:          formatTMDBSeasons(tmdbSlice(result, "seasons")),
	}
}

func tmdbTitle(result map[string]any) string {
	if title := tmdbString(result, "title"); title != "" {
		return title
	}
	return tmdbString(result, "name")
}

func tmdbOriginalTitle(result map[string]any) string {
	if title := tmdbString(result, "original_title"); title != "" {
		return title
	}
	return tmdbString(result, "original_name")
}

func formatTMDBEpisode(episode map[string]any) *TMDBFormattedEpisode {
	if episode == nil {
		return nil
	}
	return &TMDBFormattedEpisode{
		Season:  tmdbInt(episode, "season_number"),
		Episode: tmdbInt(episode, "episode_number"),
		Name:    tmdbString(episode, "name"),
		AirDate: tmdbString(episode, "air_date"),
		Runtime: tmdbInt(episode, "runtime"),
	}
}

func formatTMDBSeasons(values []any) []TMDBFormattedSeason {
	seasons := make([]TMDBFormattedSeason, 0, len(values))
	regularOrder := 0
	for _, value := range values {
		season, ok := value.(map[string]any)
		if !ok {
			continue
		}
		seasonNumber := tmdbInt(season, "season_number")
		formatted := TMDBFormattedSeason{
			Season:      seasonNumber,
			Name:        tmdbString(season, "name"),
			Episodes:    tmdbInt(season, "episode_count"),
			AirDate:     tmdbString(season, "air_date"),
			VoteAverage: tmdbFloat(season, "vote_average"),
			Specials:    seasonNumber == 0,
			EpisodeList: formatTMDBEpisodeList(tmdbSlice(season, "episodes")),
		}
		if seasonNumber > 0 {
			regularOrder++
			formatted.RegularOrder = regularOrder
		}
		seasons = append(seasons, formatted)
	}
	return seasons
}

func formatTMDBEpisodeList(values []any) []TMDBFormattedEpisode {
	episodes := make([]TMDBFormattedEpisode, 0, len(values))
	for _, value := range values {
		episode, ok := value.(map[string]any)
		if !ok {
			continue
		}
		episodes = append(episodes, TMDBFormattedEpisode{
			Season:  tmdbInt(episode, "season_number"),
			Episode: tmdbInt(episode, "episode_number"),
			Name:    tmdbString(episode, "name"),
			AirDate: tmdbString(episode, "air_date"),
			Runtime: tmdbInt(episode, "runtime"),
		})
	}
	return episodes
}

func tmdbLess(mediaType TMDBMediaType, left, right map[string]any) bool {
	leftYear := tmdbYear(left)
	rightYear := tmdbYear(right)
	if leftYear != rightYear {
		return leftYear > rightYear
	}
	if mediaType == TMDBMediaMovie {
		leftVote := tmdbVoteAverage(left)
		rightVote := tmdbVoteAverage(right)
		if leftVote != rightVote {
			return leftVote > rightVote
		}
	}
	return false
}

func (c *TMDBClient) SearchAggregated(ctx context.Context, mediaType TMDBMediaType, query string) (TMDBSearchResponse, error) {
	resp, err := c.Search(ctx, mediaType, query)
	if err != nil {
		return TMDBSearchResponse{}, err
	}
	if mediaType != TMDBMediaTV {
		return resp, nil
	}

	for _, result := range resp.Results {
		id, ok := tmdbID(result)
		if !ok {
			continue
		}
		details, err := c.detail(ctx, mediaType, id)
		if err != nil {
			return TMDBSearchResponse{}, err
		}
		mergeTMDBTVDetails(result, details)
		if err := c.mergeTMDBSeasonDetails(ctx, id, result); err != nil {
			return TMDBSearchResponse{}, err
		}
	}
	return resp, nil
}

func tmdbID(result map[string]any) (int, bool) {
	value := tmdbInt(result, "id")
	if value == 0 {
		return 0, false
	}
	return value, true
}

func mergeTMDBTVDetails(result map[string]any, details map[string]any) {
	for _, field := range []string{
		"episode_run_time",
		"in_production",
		"last_air_date",
		"last_episode_to_air",
		"next_episode_to_air",
		"number_of_episodes",
		"number_of_seasons",
		"seasons",
		"status",
		"type",
	} {
		if value, ok := details[field]; ok {
			result[field] = value
		}
	}
}

func (c *TMDBClient) mergeTMDBSeasonDetails(ctx context.Context, tvID int, result map[string]any) error {
	seasons := tmdbSlice(result, "seasons")
	for _, value := range seasons {
		season, ok := value.(map[string]any)
		if !ok {
			continue
		}
		seasonNumber := tmdbInt(season, "season_number")
		details, err := c.season(ctx, tvID, seasonNumber)
		if err != nil {
			return err
		}
		if episodes, ok := details["episodes"]; ok {
			season["episodes"] = episodes
		}
	}
	return nil
}

func tmdbYear(result map[string]any) int {
	for _, field := range []string{"release_date", "first_air_date"} {
		value, ok := result[field].(string)
		if !ok || len(value) < 4 {
			continue
		}
		year, err := strconv.Atoi(value[:4])
		if err == nil {
			return year
		}
	}
	return 0
}

func tmdbVoteAverage(result map[string]any) float64 {
	return tmdbFloat(result, "vote_average")
}

func tmdbString(result map[string]any, field string) string {
	value, ok := result[field].(string)
	if !ok {
		return ""
	}
	return value
}

func tmdbInt(result map[string]any, field string) int {
	value, ok := result[field].(float64)
	if !ok {
		return 0
	}
	return int(value)
}

func tmdbFloat(result map[string]any, field string) float64 {
	value, ok := result[field].(float64)
	if !ok {
		return 0
	}
	return value
}

func tmdbMap(result map[string]any, field string) map[string]any {
	value, ok := result[field].(map[string]any)
	if !ok {
		return nil
	}
	return value
}

func tmdbSlice(result map[string]any, field string) []any {
	value, ok := result[field].([]any)
	if !ok {
		return nil
	}
	return value
}

func (c *TMDBClient) searchPath(ctx context.Context, path, query string) (TMDBSearchResponse, error) {
	u, err := url.Parse(c.apiURL + path)
	if err != nil {
		return TMDBSearchResponse{}, fmt.Errorf("build tmdb url: %w", err)
	}

	values := u.Query()
	values.Set("query", query)
	values.Set("language", c.language)
	values.Set("include_adult", "false")
	values.Set("page", "1")
	if !c.bearer {
		values.Set("api_key", c.apiKey)
	}
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return TMDBSearchResponse{}, fmt.Errorf("build tmdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mvdl/1.0")
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return TMDBSearchResponse{}, provider.NewRequestError("tmdb", req, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return TMDBSearchResponse{}, provider.NewStatusError("tmdb", req, httpResp.StatusCode, strings.TrimSpace(string(msg)))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return TMDBSearchResponse{}, fmt.Errorf("read tmdb response: %w", err)
	}

	var out TMDBSearchResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return TMDBSearchResponse{}, fmt.Errorf("decode tmdb response: %w", err)
	}
	return out, nil
}

func (c *TMDBClient) detail(ctx context.Context, mediaType TMDBMediaType, id int) (map[string]any, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%d", c.apiURL, mediaType, id))
	if err != nil {
		return nil, fmt.Errorf("build tmdb detail url: %w", err)
	}
	return c.get(ctx, u, "tmdb detail")
}

func (c *TMDBClient) season(ctx context.Context, tvID, seasonNumber int) (map[string]any, error) {
	u, err := url.Parse(fmt.Sprintf("%s/tv/%d/season/%d", c.apiURL, tvID, seasonNumber))
	if err != nil {
		return nil, fmt.Errorf("build tmdb season url: %w", err)
	}
	return c.get(ctx, u, "tmdb season")
}

func (c *TMDBClient) get(ctx context.Context, u *url.URL, label string) (map[string]any, error) {
	values := u.Query()
	values.Set("language", c.language)
	if !c.bearer {
		values.Set("api_key", c.apiKey)
	}
	u.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", label, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mvdl/1.0")
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, provider.NewRequestError("tmdb", req, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return nil, provider.NewStatusError("tmdb", req, httpResp.StatusCode, strings.TrimSpace(string(msg)))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", label, err)
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", label, err)
	}
	return out, nil
}
