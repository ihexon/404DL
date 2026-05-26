package server

import (
	"strings"
	"testing"

	"mvdl/internal/model"
)

type stubEncryptor struct{}

func (stubEncryptor) EncryptString(plaintext string) (string, error) {
	return "encrypted:" + plaintext, nil
}

func TestEncryptMagnetsDoesNotMutateInput(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:abc"
	h := NewHandler(nil, Config{MagnetEncryptor: stubEncryptor{}})
	hits := []model.Torrent{{Title: "movie", MagnetURL: &magnet}}

	encrypted, err := h.encryptMagnets(hits)
	if err != nil {
		t.Fatal(err)
	}
	if got := *hits[0].MagnetURL; got != magnet {
		t.Fatalf("input magnet = %q, want %q", got, magnet)
	}
	if got := *encrypted[0].MagnetURL; !strings.HasPrefix(got, "encrypted:") {
		t.Fatalf("encrypted magnet = %q, want encrypted prefix", got)
	}
}
