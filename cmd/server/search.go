package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"4dl/internal/logging"
)

func runSearch(c *cli.Context) error {
	startedAt := time.Now()
	searchQuery := c.Args().First()
	if searchQuery == "" {
		return fmt.Errorf("query is required")
	}

	providerFilter := c.StringSlice(FlagProvider)
	endpoint, err := searchAPIEndpoint(c)
	if err != nil {
		return err
	}
	defer endpoint.Close()
	client := &http.Client{Timeout: searchAPIRequestTimeout(c.Duration(FlagTimeout), endpoint.Embedded)}

	requestURL, err := searchAPIURL(endpoint.URL, searchQuery, c.Int(FlagLimitSize), providerFilter)
	if err != nil {
		return err
	}

	requestID := logging.NewRequestID()
	logrus.WithFields(logrus.Fields{
		"request_id":      requestID,
		"query":           searchQuery,
		"provider_filter": providerFilterLogValue(providerFilter),
		"limit":           c.Int(FlagLimitSize),
		"server_url":      endpoint.URL,
		"embedded_server": endpoint.Embedded,
		"timeout":         client.Timeout.String(),
	}).Info("search API request started")

	httpReq, err := http.NewRequestWithContext(c.Context, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build search API request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set(logging.RequestIDHeader, requestID)

	resp, err := client.Do(httpReq)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"request_id":      requestID,
			"query":           searchQuery,
			"provider_filter": providerFilterLogValue(providerFilter),
			"duration_ms":     logging.DurationMillis(time.Since(startedAt)),
		}).Error("search API request failed")
		return fmt.Errorf("call search API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		logrus.WithFields(logrus.Fields{
			"request_id":      requestID,
			"query":           searchQuery,
			"provider_filter": providerFilterLogValue(providerFilter),
			"http_status":     resp.StatusCode,
			"duration_ms":     logging.DurationMillis(time.Since(startedAt)),
		}).Error("search API request rejected")
		return fmt.Errorf("search API returned HTTP %d: %s", resp.StatusCode, message)
	}

	written, err := io.Copy(os.Stdout, resp.Body)
	if err != nil {
		return fmt.Errorf("write search API response: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"request_id":      requestID,
		"query":           searchQuery,
		"provider_filter": providerFilterLogValue(providerFilter),
		"bytes":           written,
		"duration_ms":     logging.DurationMillis(time.Since(startedAt)),
	}).Info("search API request completed")

	return nil
}

type searchAPIEndpointConfig struct {
	URL      string
	Embedded bool
	close    func()
}

func (c searchAPIEndpointConfig) Close() {
	if c.close != nil {
		c.close()
	}
}

func searchAPIEndpoint(c *cli.Context) (searchAPIEndpointConfig, error) {
	externalURL := strings.TrimSpace(c.String(FlagServerURL))
	if externalURL != "" {
		return searchAPIEndpointConfig{URL: externalURL}, nil
	}

	cfg, err := newSearchServerConfig(c.Int(FlagLimitSize), c.Duration(FlagTimeout), false)
	if err != nil {
		return searchAPIEndpointConfig{}, err
	}
	ts := httptest.NewServer(newSearchHandler(cfg).Routes())
	return searchAPIEndpointConfig{
		URL:      ts.URL,
		Embedded: true,
		close:    ts.Close,
	}, nil
}

func providerFilterLogValue(providers []string) any {
	if len(providers) == 0 {
		return "all"
	}
	return providers
}

func searchAPIRequestTimeout(searchTimeout time.Duration, embedded bool) time.Duration {
	if !embedded || searchTimeout <= 0 {
		return searchTimeout
	}
	return searchTimeout + time.Second
}

func searchAPIURL(serverURL, query string, limit int, providers []string) (string, error) {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		return "", fmt.Errorf("server URL is required")
	}
	u, err := url.Parse(serverURL + "/v1/search")
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("server URL must include scheme and host")
	}

	values := u.Query()
	values.Set("q", strings.TrimSpace(query))
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	for _, providerName := range providers {
		providerName = strings.ToLower(strings.TrimSpace(providerName))
		if providerName == "" {
			continue
		}
		values.Add("provider", providerName)
	}
	u.RawQuery = values.Encode()
	return u.String(), nil
}
