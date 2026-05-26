package domain

import (
	"context"
	"mvdl/internal/model"
	"mvdl/internal/provider"
)

type TorrentSearcher interface {
	Search(ctx context.Context, req provider.SearchRequest) ([]model.Torrent, error)
}

type StringEncryptor interface {
	GenKey() (string, error)
	EncryptString(plaintext string) (string, error)
	DecryptString(encoded string) (string, error)
}
