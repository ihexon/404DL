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

	"4dl/internal/crypto"
	"4dl/internal/logging"
	"4dl/internal/responsecodec"
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
	decryptor, requireEncrypted, err := newResponseDecryptor()
	if err != nil {
		return err
	}

	requestURL, err := searchAPIURL(endpoint.URL, searchQuery, c.Int(FlagLimitSize), providerFilter)
	if err != nil {
		return err
	}

	requestID := logging.NewRequestID()
	logrus.WithFields(logrus.Fields{
		"request_id":                 requestID,
		"query":                      searchQuery,
		"provider_filter":            providerFilterLogValue(providerFilter),
		"limit":                      c.Int(FlagLimitSize),
		"server_url":                 endpoint.URL,
		"embedded_server":            endpoint.Embedded,
		"require_encrypted_response": requireEncrypted,
		"timeout":                    client.Timeout.String(),
	}).Info("search API request started")

	httpReq, err := http.NewRequestWithContext(c.Context, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build search API request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set(logging.RequestIDHeader, requestID)
	if requireEncrypted {
		responsecodec.RequireEncryptedResponse(httpReq)
	}

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

	body, encrypted, err := readSearchAPIResponse(resp, decryptor, requireEncrypted)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := strings.TrimSpace(string(limitMessageBody(body, 4096)))
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

	written, err := os.Stdout.Write(body)
	if err != nil {
		return fmt.Errorf("write search API response: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"request_id":         requestID,
		"query":              searchQuery,
		"provider_filter":    providerFilterLogValue(providerFilter),
		"response_encrypted": encrypted,
		"bytes":              written,
		"duration_ms":        logging.DurationMillis(time.Since(startedAt)),
	}).Info("search API request completed")

	return nil
}

func newResponseDecryptor() (*crypto.StringEncryptor, bool, error) {
	key := secretEnvString("", envCryptoKey)
	if key == "" {
		return nil, false, nil
	}
	decryptor, err := crypto.NewStringEncryptor(key)
	if err != nil {
		return nil, true, fmt.Errorf("invalid %s: %w", envCryptoKey, err)
	}
	return decryptor, true, nil
}

func readSearchAPIResponse(resp *http.Response, decryptor *crypto.StringEncryptor, requireEncrypted bool) ([]byte, bool, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read search API response: %w", err)
	}
	decoded, encrypted, err := responsecodec.DecodeHTTPBody(body, resp.Header, decryptor, requireEncrypted)
	if err != nil {
		return nil, encrypted, fmt.Errorf("decode search API response: %w", err)
	}
	return decoded, encrypted, nil
}

func limitMessageBody(body []byte, limit int) []byte {
	if len(body) <= limit {
		return body
	}
	return body[:limit]
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
