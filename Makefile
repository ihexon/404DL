.DEFAULT_GOAL := build

BINARY ?= 4dl
BUILD_DIR ?= bin
DIST_DIR ?= dist
CMD ?= ./cmd/server
GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 1
CC ?= cc
CXX ?= c++
LDFLAGS ?= -s -w
NPM ?= npm
WEB_DIR ?= web/get
WEB_STATIC_DIR ?= internal/get/static
BIN_PATH := $(BUILD_DIR)/$(BINARY)
RELEASE_NAME ?= $(BINARY)-$(GOOS)-$(GOARCH)
RELEASE_DIR := $(DIST_DIR)/$(RELEASE_NAME)

.PHONY: build binary clean clean-binary clean-dist clean-web release-package web web-deps

build: binary

web: web-deps
	cd $(WEB_DIR) && $(NPM) run build

web-deps:
	cd $(WEB_DIR) && $(NPM) ci

binary: web
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) CC=$(CC) CXX=$(CXX) $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_PATH) $(CMD)

release-package: build
	rm -rf $(RELEASE_DIR)
	mkdir -p $(RELEASE_DIR)
	cp $(BIN_PATH) $(RELEASE_DIR)/
	if [ "$(GOOS)" = "windows" ]; then \
		cd $(DIST_DIR) && zip -r $(RELEASE_NAME).zip $(RELEASE_NAME); \
	else \
		cd $(DIST_DIR) && tar -czf $(RELEASE_NAME).tar.gz $(RELEASE_NAME); \
	fi

clean: clean-binary clean-dist clean-web

clean-binary:
	rm -rf $(BUILD_DIR)

clean-dist:
	rm -rf $(DIST_DIR)

clean-web:
	rm -rf $(WEB_STATIC_DIR)/assets $(WEB_STATIC_DIR)/index.html
