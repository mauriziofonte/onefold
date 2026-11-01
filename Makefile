# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# onefold — cross-platform build.
# The indexer/planner/applier are a single static binary that runs where the
# files live: Linux (WSL, a NAS), macOS (Apple Silicon and Intel) and
# Windows-native (to index a Windows drive without the WSL 9P bridge).
# Functional parity across OSes is a hard requirement, so we depend on NO
# external tools for transport (no rsync/ssh binary).

BIN     := onefold
PKG     := ./cmd/onefold
DIST    := dist
# Version (SemVer, see VERSION and CHANGELOG.md): the file VERSION holds the
# version being prepared. A clean checkout of its tag v<VERSION> builds as
# v<VERSION>; any other tree builds as v<VERSION>+dev.<commit>[.dirty], so a
# development binary can never pass for the release. `make VERSION=...`
# overrides it.
BASE    := v$(shell cat VERSION)
TAGGED  := $(shell git describe --tags --exact-match 2>/dev/null)
COMMIT  := $(or $(shell git rev-parse --short HEAD 2>/dev/null),unknown)
DIRTY   := $(shell git status --porcelain --untracked-files=no 2>/dev/null | grep -q . && echo .dirty)
VERSION := $(if $(and $(filter $(BASE),$(TAGGED)),$(if $(DIRTY),,clean)),$(BASE),$(BASE)+dev.$(COMMIT)$(DIRTY))
LDFLAGS := -s -w -X main.version=$(VERSION)

# Static by default: CGO off so the binary runs on any host with zero deps.
export CGO_ENABLED := 0

.PHONY: version build test vet lint race cli-demo all linux windows linux-arm64 darwin release notices winres workflows-check dist-clean web web-e2e web-demo

version: ## the version a build of this tree gets
	@echo $(VERSION)

build: ## host binary
	go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

# What CI runs besides the tests: staticcheck (pinned) for every target OS, and
# the race detector (needs cgo and a C compiler, unlike the product builds).
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2
lint:
	for goos in linux windows darwin; do GOOS=$$goos go run $(STATICCHECK) ./... || exit 1; done

race:
	CGO_ENABLED=1 go test -race -count=1 ./...

# GUI frontend (Svelte + Vite): rebuilds internal/gui/web/dist, which the Go
# binary embeds and which is committed (go build/test need no Node).
web:
	cd internal/gui/web && npm ci && npm run build

# GUI end-to-end on Linux: real binary (built to a temp path, the host build
# ./onefold stays untouched), real browser (Playwright), scratch data under E2E_ROOT.
# README demo of the GUI: assets/demo/gui-merge.gif (Node, ffmpeg; in CI, with
# the Windows and macOS GIFs: .github/workflows/demo.yml)
web-demo:
	scripts/gui-demo.sh

# README demos of the CLI: assets/demo/cli-*.gif (vhs with ttyd, ffmpeg, exiftool;
# in CI: .github/workflows/demo.yml)
cli-demo:
	scripts/tapes/render.sh

web-e2e:
	go build -o $${TMPDIR:-/tmp}/onefold-e2e ./cmd/onefold
	cd internal/gui/web && ONEFOLD_BIN=$${TMPDIR:-/tmp}/onefold-e2e npm run e2e

all: linux windows ## the two targets we actually deploy

# Release artifacts: every deploy target, the license, the third-party notices,
# SHA-256 sums (.github/workflows/release.yml publishes them on a version tag).
release: linux windows linux-arm64 darwin notices
	cp LICENSE $(DIST)/LICENSE
	cd $(DIST) && sha256sum $(BIN)-linux-amd64 $(BIN)-windows-amd64.exe $(BIN)-linux-arm64 $(BIN)-darwin-arm64 $(BIN)-darwin-amd64 LICENSE THIRD_PARTY_NOTICES.txt > SHA256SUMS

# Licenses of everything linked or bundled into the binaries: Go standard
# library, every module onefold imports on Linux, Windows and macOS, the GUI
# bundle.
notices:
	mkdir -p $(DIST)
	{ echo "Third-party components of onefold $(VERSION). onefold itself: see LICENSE."; echo; \
	  echo "=== Go standard library and runtime ($$(go env GOVERSION))"; cat "$$(go env GOROOT)/LICENSE"; echo; \
	  { GOOS=linux go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' $(PKG); \
	    GOOS=windows go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' $(PKG); \
	    GOOS=darwin go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' $(PKG); } | sort -u | \
	  while read -r mod ver dir; do echo "=== $$mod $$ver"; cat "$$dir"/LICENSE* "$$dir"/LICENCE* "$$dir"/COPYING* "$$dir"/UNLICENSE* 2>/dev/null; echo; done; \
	  echo "=== GUI frontend (bundled JavaScript and CSS)"; cat internal/gui/web/dist/THIRD_PARTY_LICENSES.md; } > $(DIST)/THIRD_PARTY_NOTICES.txt

# GitHub workflows checked before they reach GitHub: linters, then act runs of
# CI and release on a clean clone (scripts/check-workflows.sh; needs docker).
workflows-check:
	scripts/check-workflows.sh $(TAG)

# Windows resources of the exe (icon + manifest, internal/winres): writes the
# committed cmd/onefold/rsrc_windows_amd64.syso (TestSysoInSync guards it).
winres:
	go generate ./cmd/onefold

linux: ## Linux / WSL / NAS (amd64, static)
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-linux-amd64      $(PKG)

# The version of the build goes into the exe's version information (Explorer,
# Task Manager): the committed .syso is regenerated with it for this build and
# restored afterwards, also when the build fails.
SYSO := cmd/onefold/rsrc_windows_amd64.syso

windows: ## native Windows (amd64) — indexes D:\ without 9P
	@cp $(SYSO) $(SYSO).committed
	@trap 'mv -f $(SYSO).committed $(SYSO)' EXIT; \
	  (cd cmd/onefold && go run gen_winres.go -version $(VERSION)) && \
	  GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-windows-amd64.exe $(PKG)

linux-arm64: ## (e.g. an ARM NAS) — trivial extra target
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-linux-arm64      $(PKG)

darwin: ## macOS, Apple Silicon and Intel (the linker signs arm64 ad hoc, as macOS requires)
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-darwin-arm64     $(PKG)
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-darwin-amd64     $(PKG)

dist-clean:
	go clean
	rm -f $(BIN) $(DIST)/$(BIN)-* $(DIST)/SHA256SUMS $(DIST)/THIRD_PARTY_NOTICES.txt $(DIST)/LICENSE
