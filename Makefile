.DEFAULT_GOAL := build

BINARY ?= mvdl
BUILD_DIR ?= bin
CMD ?= ./cmd/server
GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 0
LDFLAGS ?= -s -w

.PHONY: build clean test

build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(CMD)

test:
	$(GO) test ./...

clean:
	rm -rf $(BUILD_DIR)
