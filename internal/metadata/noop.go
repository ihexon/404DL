package metadata

import "context"

type NoopResolver struct{}

func (NoopResolver) ResolveMovie(_ context.Context, _ string) (Movie, error) {
	return Movie{}, nil
}
