package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	downloadui "mvdl/internal/get"
)

func runGet(c *cli.Context) error {
	inputPath, ok, err := getInputPath(c)
	if err != nil || !ok {
		return err
	}

	saveTo, err := getSaveTo(c.String(FlagSaveTo))
	if err != nil {
		return err
	}

	cfg := downloadui.Config{
		ListenAddr:        c.String(FlagListen),
		InputPath:         inputPath,
		SaveTo:            saveTo,
		TorrentListenAddr: c.String(FlagTorrentListen),
		CryptoKey:         envString(envCryptoKey, ""),
	}

	logrus.WithFields(logrus.Fields{
		"listen":  cfg.ListenAddr,
		"input":   cfg.InputPath,
		"save_to": cfg.SaveTo,
	}).Info("get starting")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return downloadui.Run(ctx, cfg)
}

func getInputPath(c *cli.Context) (string, bool, error) {
	hasInput := c.IsSet(FlagInput)
	hasStdin := c.Bool(FlagStdin)
	if hasInput && hasStdin {
		return "", false, fmt.Errorf("--%s and --%s cannot be used together", FlagInput, FlagStdin)
	}
	if hasInput {
		return c.String(FlagInput), true, nil
	}
	if hasStdin {
		return "-", true, nil
	}
	if err := cli.ShowSubcommandHelp(c); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func getSaveTo(value string) (string, error) {
	if value != "" {
		return value, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	return filepath.Join(wd, "mvdl-downloads"), nil
}
