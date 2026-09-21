# SPDX-License-Identifier: MIT
#
# Copyright (C) 2018-2019 Jason A. Donenfeld <Jason@zx2c4.com>. All Rights Reserved.

# Select the pinned toolchain explicitly; do not search Homebrew's PATH.
GO ?= /usr/local/go/bin/go
CACHE_DIR ?= $(CURDIR)/../../build/GoBridge
OUTPUT_DIR ?= $(CURDIR)/../../Artifacts
MACOS_ARCHS ?= arm64 x86_64
SIMULATOR_ARCHS ?= arm64 x86_64
MACOS_DEPLOYMENT_TARGET ?= 13.0
IOS_DEPLOYMENT_TARGET ?= 15.0

export GOENV := off
export GOTOOLCHAIN := local
export GOWORK := off
export GOFLAGS :=

BRIDGE_INPUTS := go.mod go.sum $(wildcard *.go) wireguard.h module.modulemap \
	goruntime-boottime-over-monotonic.diff Makefile $(wildcard build-bridge/*.go) \
	../../.go-version ../../.xcode-version
HOST_GO = /usr/bin/env -u GOROOT -u GOOS -u GOARCH -u GOEXPERIMENT \
	-u GOAMD64 -u GOARM64 CGO_ENABLED=0 "$(GO)"

.PHONY: xcframework build test
xcframework build: $(BRIDGE_INPUTS)
	@case "$(GO)" in /*) ;; *) echo 'GO must be an absolute path'; exit 1 ;; esac
	$(HOST_GO) run ./build-bridge -go "$(GO)" -cache "$(CACHE_DIR)" -output "$(OUTPUT_DIR)" \
		-macos-archs "$(MACOS_ARCHS)" -simulator-archs "$(SIMULATOR_ARCHS)" \
		-macos-min "$(MACOS_DEPLOYMENT_TARGET)" -ios-min "$(IOS_DEPLOYMENT_TARGET)"

test:
	$(HOST_GO) test ./build-bridge
