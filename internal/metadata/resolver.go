package metadata

import "context"

type Movie struct {
	Title string
	Year  int
}

func (m Movie) SearchQuery(fallback string) string {
	if m.Title == "" {
		return fallback
	}
	if m.Year == 0 {
		return m.Title
	}
	return m.Title + " " + itoa(m.Year)
}

type Resolver interface {
	ResolveMovie(ctx context.Context, query string) (Movie, error)
}

type NoopResolver struct{}

func (NoopResolver) ResolveMovie(_ context.Context, _ string) (Movie, error) {
	return Movie{}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
