package metadata

import (
	"context"
	"strconv"
)

type Movie struct {
	Title string
	Year  int
}

// SearchQuery generates a search-friendly string based on the movie's title and year. Returns an empty string if the title is empty.
func (m Movie) SearchQuery() string {
	if m.Title == "" {
		return ""
	}

	if m.Year == 0 {
		return m.Title
	}

	return m.Title + " " + strconv.Itoa(m.Year)
}

type Resolver interface {
	ResolveMovie(ctx context.Context, query string) (Movie, error)
}
