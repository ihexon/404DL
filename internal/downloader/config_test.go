package downloader

import (
	"strings"
	"testing"
)

func TestConfigValidateRequiresDataDir(t *testing.T) {
	err := (Config{}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "data directory") {
		t.Fatalf("Validate() error = %q, want data directory error", err)
	}
}

func TestConfigValidateRejectsNegativeLimits(t *testing.T) {
	cfg := Config{
		DataDir:                 ".",
		DownloadRateBytesPerSec: -1,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "download rate") {
		t.Fatalf("Validate() error = %q, want download rate error", err)
	}
}
