package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func runSearch(c *cli.Context) error {
	searchName := c.Args().First()
	if searchName == "" {
		return fmt.Errorf("movie name is required")
	}

	baseURL, err := searchURL(c, searchName)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: time.Duration(c.Int(FlagTimeout)) * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil {
		return fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mvdl/1.0")

	logrus.WithFields(logrus.Fields{
		"addr":       c.String(FlagAddr),
		"search":     searchName,
		"resolution": c.String(FlagResolution),
	}).Info("search request started")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call search API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("search API returned %d: %s", resp.StatusCode, string(body))
	}

	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("write search response: %w", err)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func searchURL(c *cli.Context, searchName string) (string, error) {
	baseURL, err := url.Parse(c.String(FlagAddr))
	if err != nil {
		return "", fmt.Errorf("parse addr: %w", err)
	}

	baseURL.Path = "/v1/t"
	query := baseURL.Query()
	query.Set("search", searchName)
	query.Set("resolution", c.String(FlagResolution))
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}
