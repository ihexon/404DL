package search

import (
	"context"

	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type ProviderSearcher interface {
	Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error)
}

type Service struct {
	provider ProviderSearcher
}

func NewService(provider ProviderSearcher) *Service {
	return &Service{
		provider: provider,
	}
}

func (s *Service) Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error) {
	return s.provider.Search(ctx, req)
}
