BUILD_DIR := build

# ── Go binaries ─────────────────────────────────────
VERSION  ?= dev
LDFLAGS  := -ldflags "-X github.com/marcelocantos/jevons/internal/cli.Version=$(VERSION)"
GO_SRC   := $(shell find cmd internal -name '*.go' 2>/dev/null)
EMBED_GUIDE := internal/cli/help_agent.md

$(EMBED_GUIDE): agents-guide.md
	cp $< $@

.PHONY: all
all: jevonsd

.PHONY: jevonsd
jevonsd: bin/jevonsd

bin/jevonsd: $(GO_SRC) $(EMBED_GUIDE)
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/jevonsd ./cmd/jevonsd

# ── Run ──────────────────────────────────────────────
.PHONY: run run-jevonsd
run-jevonsd: bin/jevonsd
	bin/jevonsd

run: run-jevonsd

# ── Setup ────────────────────────────────────────────
.PHONY: init
init:
	@echo "── jevons project setup ──"
	@command -v go >/dev/null 2>&1 || { echo "ERROR: Go not found. Install from https://go.dev/dl/"; exit 1; }
	@echo "  Go: $$(go version)"
	@go mod download
	@echo "  Go dependencies downloaded"

# ── iOS app ─────────────────────────────────────────
.PHONY: ios
ios:
	cd ios && xcodegen generate

# ── Test ─────────────────────────────────────────────
.PHONY: test test-go test-web test-ui
test-go:
	go test ./...

# Hermetic Node tests for chat working-indicator lifecycle (🎯T39).
test-web:
	node web/scripts/chat_events_test.js

# Playwright perceptual chat UI (hermetic mocked WS; needs playwright
# from scripts/browser-loop-test). Live: make test-ui-live.
test-ui:
	node scripts/chat-ui-test/test.js
	node scripts/chat-ui-test/collapse-test.js
	node scripts/chat-ui-test/infinite-scroll-test.js

.PHONY: test-ui-live
test-ui-live:
	node scripts/chat-ui-test/test.js --live

# Live scenario suite (🎯T51): drives a RUNNING jevonsd through the
# owner flows. Deterministic tier only by default; see scripts/live-suite
# flags for the overseer/spawn/restart/rewind scenarios.
.PHONY: test-live-suite
test-live-suite:
	go run ./scripts/live-suite -skip-overseer

test: test-go test-web test-ui

# ── Standing invariants (bullseye) ──────────────────
.PHONY: bullseye
bullseye:
	@go build ./... && echo "✓ build"
	@go test ./... && echo "✓ tests"
	@go vet ./... && echo "✓ vet"
	@test -z "$$(git status --porcelain)" && echo "✓ clean" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)
