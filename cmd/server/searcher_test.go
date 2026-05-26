package main

import (
	"net/http"
	"testing"
)

func TestNewSearchProvidersSelectsSingleProvider(t *testing.T) {
	providers, err := newSearchProviders(http.DefaultClient, "knaben")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(providers))
	}
	if got := providers[0].Name(); got != "knaben" {
		t.Fatalf("provider = %q, want knaben", got)
	}
}

func TestNewSearchProvidersSelectsMultipleProviders(t *testing.T) {
	providers, err := newSearchProviders(http.DefaultClient, "torrentclaw", "knaben")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(providers))
	}
	got := map[string]bool{}
	for _, provider := range providers {
		got[provider.Name()] = true
	}
	for _, name := range []string{"knaben", "torrentclaw"} {
		if !got[name] {
			t.Fatalf("selected providers = %#v, missing %s", got, name)
		}
	}
}

func TestNewSearchProvidersDefaultsToAllProviders(t *testing.T) {
	providers, err := newSearchProviders(http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != len(providerFactories) {
		t.Fatalf("providers = %d, want %d", len(providers), len(providerFactories))
	}
}
