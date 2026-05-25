package main

import (
	"flag"
	"net/http"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"mvdl/internal/knaben"
	"mvdl/internal/metadata"
	"mvdl/internal/provider"
	"mvdl/internal/search"
	"mvdl/internal/server"
	"mvdl/internal/torrentclaw"
)

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	listen := flag.String("listen", envString("ADDR", ":8080"), "listen address, for example :8080 or 127.0.0.1:8080")
	flag.Parse()

	cfg := server.Config{
		Addr:       *listen,
		PageSize:   envInt("PAGE_SIZE", 200),
		HTTPClient: &http.Client{Timeout: envDuration("UPSTREAM_TIMEOUT", 8*time.Second)},
	}

	providers := []provider.Provider{
		knaben.NewClient(envString("KNABEN_API_URL", "https://api.knaben.org/v1"), cfg.HTTPClient),
		torrentclaw.NewClient(envString("TORRENTCLAW_API_URL", "https://torrentclaw.com/api/v1"), cfg.HTTPClient),
	}
	aggregator := provider.NewAggregator(providers...)
	resolver := metadata.Resolver(metadata.NoopResolver{})
	if apiKey := envString("MVDL_TMDB_APIKEY", ""); apiKey != "" {
		log.WithFields(log.Fields{
			"api_key": maskAPIKey(apiKey),
			"api_url": envString("TMDB_API_URL", "https://api.themoviedb.org/3"),
		}).Info("tmdb resolver enabled")
		resolver = metadata.NewTMDBClient(metadata.TMDBOptions{
			APIURL:     envString("TMDB_API_URL", "https://api.themoviedb.org/3"),
			APIKey:     apiKey,
			HTTPClient: cfg.HTTPClient,
		})
	} else {
		log.Info("tmdb resolver disabled: MVDL_TMDB_APIKEY is not set")
	}
	searcher := search.NewService(resolver, aggregator)
	handler := server.NewHandler(searcher, cfg)

	log.WithFields(log.Fields{
		"listen":  cfg.Addr,
		"limit":   cfg.PageSize,
		"timeout": cfg.HTTPClient.Timeout.String(),
	}).Info("server listening")
	if err := http.ListenAndServe(cfg.Addr, handler.Routes()); err != nil {
		log.WithError(err).Fatal("server stopped")
	}
}

func envString(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func maskAPIKey(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:len(value)-4] + "****"
}
