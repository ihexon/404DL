package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"mvdl/internal/server"
)

func runSearch(c *cli.Context) error {
	searchName := c.Args().First()
	if searchName == "" {
		return fmt.Errorf("movie name is required")
	}

	client := &http.Client{
		Timeout: time.Duration(c.Int(FlagTimeout)) * time.Second,
	}
	cfg, err := newServerConfig(c)
	if err != nil {
		return err
	}
	cfg.Addr = "127.0.0.1:0"
	cfg.HTTPClient = client

	baseURL, shutdown, err := serveQuery(cfg)
	if err != nil {
		return err
	}
	defer shutdown()

	searchURL, err := queryURL(baseURL, searchName, c.String(FlagResolution))
	if err != nil {
		return err
	}

	logrus.WithFields(logrus.Fields{
		"search":     searchName,
		"resolution": c.String(FlagResolution),
	}).Info("search request started")

	req, err := http.NewRequestWithContext(c.Context, http.MethodGet, searchURL, nil)
	if err != nil {
		return fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mvdl/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call query server: %w", err)
	}
	defer resp.Body.Close()

	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("write search response: %w", err)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func serveQuery(cfg server.Config) (string, func(), error) {
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen query server: %w", err)
	}

	httpServer := &http.Server{
		Handler: newSearchHandler(cfg).Routes(),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Warn("query server stopped")
		}
	}()

	shutdown := func() {
		_ = httpServer.Close()
		<-done
	}

	return "http://" + listener.Addr().String(), shutdown, nil
}

func queryURL(baseURL, searchName, resolution string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse query server URL: %w", err)
	}

	u.Path = "/v1/t"
	query := u.Query()
	query.Set("search", searchName)
	query.Set("resolution", resolution)
	u.RawQuery = query.Encode()
	return u.String(), nil
}
