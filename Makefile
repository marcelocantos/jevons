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

# Hermetic Node tests for chat working-indicator lifecycle (🎯T39)
# and attention-thread model (🎯T65).
test-web:
	node web/scripts/chat_events_test.js
	node web/scripts/attention_threads_test.js
	node web/scripts/aside_history_test.js
	node web/scripts/fleet_row_test.js
	node web/scripts/portfolio_group_test.js
	node web/scripts/virtual_list_test.js
	node web/scripts/thread_route_test.js
	node web/scripts/route_suggest_test.js
	node web/scripts/layout_probe_test.js
	node web/scripts/composer_layout_test.js
	node web/scripts/composer_keys_test.js
	node web/scripts/composer_focus_test.js
	node web/scripts/wispr_context_test.js
	node web/scripts/send_queue_test.js
	node web/scripts/composer_persist_test.js
	node web/scripts/rhs_layout_test.js
	node web/scripts/decision_log_test.js
	node web/scripts/chat_reconnect_test.js
	node web/scripts/tool_summary_test.js
	node web/scripts/working_progress_test.js
	node web/scripts/tool_tooltip_test.js
	node web/scripts/instant_tip_test.js
	node web/scripts/agent_transcript_test.js
	node web/scripts/frontier_table_test.js
	node web/scripts/target_context_chrome_test.js
	node web/scripts/mermaid_actions_test.js
	node web/scripts/markdown_normalize_test.js
	node web/scripts/streaming_markdown_test.js
	node web/scripts/cost_display_test.js
	node web/scripts/link_safety_test.js
	node web/scripts/image_lightbox_test.js

# Playwright perceptual chat UI (hermetic mocked WS; needs playwright
# from scripts/browser-loop-test). Live: make test-ui-live.
test-ui:
	node scripts/chat-ui-test/test.js
	node scripts/chat-ui-test/collapse-test.js
	node scripts/chat-ui-test/stream-scroll-test.js
	node scripts/chat-ui-test/fleet-tree-test.js
	node scripts/chat-ui-test/attention-ui-test.js
	node scripts/chat-ui-test/batch-t109-test.js
	node scripts/chat-ui-test/infinite-scroll-test.js
	node scripts/chat-ui-test/replay-scroll-test.js
	node scripts/chat-ui-test/mermaid-test.js
	node scripts/chat-ui-test/agent-note-test.js
	node scripts/chat-ui-test/t159-seal-test.js
	node scripts/chat-ui-test/virtual-list-test.js
	node scripts/chat-ui-test/image-paste-test.js
	node scripts/chat-ui-test/image-lightbox-test.js
	node scripts/chat-ui-test/t164-aside-dismiss-test.js
	node scripts/chat-ui-test/t241-alt-enter-test.js

.PHONY: test-ui-live
test-ui-live:
	node scripts/chat-ui-test/test.js --live

# Live scenario suite (🎯T51): drives a RUNNING jevonsd through the
# owner flows. Deterministic tier only by default; see scripts/live-suite
# flags for the overseer/spawn/restart/rewind scenarios.
# WARNING: defaults to your daily :13705 / ~/.jevons — prefer test-journey.
.PHONY: test-live-suite
test-live-suite:
	go run ./scripts/live-suite -skip-overseer

# Isolated owner-chat user journeys (separate port + state dir + MCP name).
# Does NOT touch daily-driver stream. Needs Grok CLI; not in default `test`.
.PHONY: test-journey
test-journey: jevonsd
	go run ./scripts/journey-suite

test: test-go test-web test-ui

# ── Standing invariants (bullseye) ──────────────────
.PHONY: bullseye
bullseye:
	@go build ./... && echo "✓ build"
	@go test ./... && echo "✓ tests"
	@go vet ./... && echo "✓ vet"
	@test -z "$$(git status --porcelain)" && echo "✓ clean" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)
