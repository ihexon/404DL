package search

import (
	"context"

	log "github.com/sirupsen/logrus"

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
		log.WithError(err).WithField("query", req.Query).Warn("metadata resolver failed; falling back to original query")
		return s.provider.Search(ctx, req)
	}
	resolvedQuery := movie.SearchQuery(req.Query)
	if resolvedQuery != req.Query {
		log.WithFields(log.Fields{
			"query":          req.Query,
			"resolved_query": resolvedQuery,
		}).Info("metadata resolver normalized query")
		req.Query = resolvedQuery
	}

	return s.provider.Search(ctx, req)
}
