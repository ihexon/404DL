package search

import (
	"context"

	"mvdl/internal/metadata"
	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type ProviderSearcher interface {
	Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error)
}

type Service struct {
	resolver metadata.Resolver
	provider ProviderSearcher
}

func NewService(resolver metadata.Resolver, provider ProviderSearcher) *Service {
	if resolver == nil {
		resolver = metadata.NoopResolver{}
	}
	return &Service{
		resolver: resolver,
		provider: provider,
	}
}

func (s *Service) Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	movie, err := s.resolver.ResolveMovie(ctx, req.Query)
	if err != nil {
		return nil, err
	}
	req.Query = movie.SearchQuery(req.Query)

	return s.provider.Search(ctx, req)
}
