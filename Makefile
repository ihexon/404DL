.DEFAULT_GOAL := build

BINARY ?= mvdl
BUILD_DIR ?= bin
CMD ?= ./cmd/server
GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 0
LDFLAGS ?= -s -w
NPM ?= npm
WEB_DIR ?= web/httpfs
WEB_STATIC_DIR ?= internal/httpfs/static
BIN_PATH := $(BUILD_DIR)/$(BINARY)

.PHONY: build binary clean clean-binary clean-web web web-deps

build: binary

web: web-deps
	cd $(WEB_DIR) && $(NPM) run build

web-deps:
	cd $(WEB_DIR) && $(NPM) ci

binary: web
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_PATH) $(CMD)

clean: clean-binary clean-web

clean-binary:
	rm -rf $(BUILD_DIR)

clean-web:
	rm -rf $(WEB_STATIC_DIR)/assets $(WEB_STATIC_DIR)/index.html
