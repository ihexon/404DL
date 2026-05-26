package provider

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestErrorFieldsUnwrapsHTTPError(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test/search", nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := fmt.Errorf("provider failed: %w", NewStatusError("example", req, http.StatusTooManyRequests, "slow down"))

	fields := ErrorFields(wrapped)

	if got := fields["provider"]; got != "example" {
		t.Fatalf("provider = %v, want example", got)
	}
	if got := fields["http_status"]; got != http.StatusTooManyRequests {
		t.Fatalf("http_status = %v, want %d", got, http.StatusTooManyRequests)
	}
	if got := fields["response_body"]; got != "slow down" {
		t.Fatalf("response_body = %v, want slow down", got)
	}

	var httpErr *HTTPError
	if !errors.As(wrapped, &httpErr) {
		t.Fatal("wrapped error does not expose HTTPError")
	}
}
