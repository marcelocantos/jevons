BUILD_DIR := build

# ── Go binaries ─────────────────────────────────────
VERSION  ?= dev
LDFLAGS  := -ldflags "-X github.com/marcelocantos/jevons/internal/cli.Version=$(VERSION)"
GO_SRC   := $(shell find cmd internal -name '*.go' 2>/dev/null)
EMBED_GUIDE := internal/cli/help_agent.md

$(EMBED_GUIDE): agents-guide.md
	cp $< $@

.PHONY: all
all: jevonsd jevons-head treeguard commitscope runlock

.PHONY: jevonsd
jevonsd: bin/jevonsd

bin/jevonsd: $(GO_SRC) $(EMBED_GUIDE)
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/jevonsd ./cmd/jevonsd

# Desktop menu-bar/tray head (🎯T27.7) — pure-Go model client.
# macOS chrome: make macos-head (Swift status item).
.PHONY: jevons-head
jevons-head: bin/jevons-head

bin/jevons-head: $(GO_SRC)
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/jevons-head ./cmd/jevons-head

# Shared-file write guard (🎯T376). The Claude Code hook in .claude/settings.json
# execs this binary on every Write/Edit, so `make all` builds it: without it the
# hook reports a visible non-blocking error instead of guarding anything.
.PHONY: treeguard
treeguard: bin/treeguard

bin/treeguard: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/treeguard ./cmd/treeguard

# Shared-index commit guard (🎯T377). The git pre-commit hook execs this, so
# `make all` builds it; the hook self-builds if it is missing, and this target
# only saves that cost on the first commit after a fresh clone. Install the
# hook itself with: cp scripts/hooks/pre-commit .git/hooks/
.PHONY: commitscope
commitscope: bin/commitscope

bin/commitscope: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/commitscope ./cmd/commitscope

# Restart serialiser (🎯T392.5). restart-daily-jevonsd re-execs itself under
# this, so a missing binary means concurrent restarts race — which is how
# the daemon was left down on 2026-08-09. Built by `make all`, and the
# script fails closed rather than restarting unserialised.
.PHONY: runlock
runlock: bin/runlock

bin/runlock: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/runlock ./cmd/runlock

.PHONY: macos-head
macos-head:
	cd macos/JevonsHead && swift build -c release

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
	node web/scripts/boot_sentinel_test.js
	node web/scripts/chat_events_test.js
	node web/scripts/owner_turn_shape_test.js
	node web/scripts/attention_threads_test.js
	node web/scripts/idea_capture_test.js
	node web/scripts/aside_history_test.js
	node web/scripts/fleet_row_test.js
	node web/scripts/fleet_paint_test.js
	node web/scripts/fleet_cycle_test.js
	node web/scripts/model_prefix_test.js
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
	node web/scripts/pending_turns_test.js
	node web/scripts/rhs_layout_test.js
	node web/scripts/decision_log_test.js
	node web/scripts/chat_reconnect_test.js
	node web/scripts/owner_ux_test.js
	node web/scripts/history_loading_test.js
	node web/scripts/tool_summary_test.js
	node web/scripts/working_progress_test.js
	node web/scripts/tool_tooltip_test.js
	node web/scripts/instant_tip_test.js
	node web/scripts/agent_transcript_test.js
	node web/scripts/conversation_widget_test.js
	node web/scripts/frontier_table_test.js
	node web/scripts/rsi_dispositions_test.js
	node web/scripts/target_context_chrome_test.js
	node web/scripts/target_hotspot_test.js
	node web/scripts/mermaid_actions_test.js
	node web/scripts/markdown_normalize_test.js
	node web/scripts/decision_matrix_test.js
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
	node scripts/chat-ui-test/t280-frontier-graph-test.js
	node scripts/chat-ui-test/t294-frontier-graph-test.js
	node scripts/chat-ui-test/agent-note-test.js
	node scripts/chat-ui-test/t159-seal-test.js
	node scripts/chat-ui-test/virtual-list-test.js
	node scripts/chat-ui-test/image-paste-test.js
	node scripts/chat-ui-test/image-lightbox-test.js
	node scripts/chat-ui-test/t164-aside-dismiss-test.js
	node scripts/chat-ui-test/t241-alt-enter-test.js
	node scripts/chat-ui-test/t289-paint-thrash-test.js
	node scripts/chat-ui-test/t341-jiggle-thrash-test.js
	node scripts/chat-ui-test/t351-fractional-pin-test.js
	node scripts/chat-ui-test/t363-scroll-up-anchor-test.js
	node scripts/chat-ui-test/t361-owner-ux-test.js
	node scripts/chat-ui-test/t309.1-conversation-widget-test.js
	node scripts/chat-ui-test/t340-frontier-table-layout-test.js
	node scripts/chat-ui-test/t366-composer-tab-cycle-test.js
	node scripts/chat-ui-test/t374-no-onerror-test.js
	node scripts/chat-ui-test/t375-boot-sentinel-test.js
	node scripts/chat-ui-test/t370-fleet-cycle-test.js
	node scripts/chat-ui-test/t369-decision-matrix-test.js
	node scripts/chat-ui-test/t368-image-prefix-route-test.js
	node scripts/chat-ui-test/t381-agent-report-markdown-test.js

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
# Does NOT touch daily-driver stream. Needs the selected provider's CLI
# (Grok by default); not in default `test`.
#
# PROVIDER selects the backend for the whole isolate — overseer and every
# agent the journeys spawn (🎯T282), e.g.:
#	make test-journey PROVIDER=claude
.PHONY: test-journey
test-journey: jevonsd
	go run ./scripts/journey-suite $(if $(PROVIDER),-provider $(PROVIDER))

test: test-go test-web test-ui

# ── Fleet spend (🎯T392.6) ──────────────────────────
# Decomposes spend into the levers that act on it:
#   tokens = turns × calls-per-turn × context-per-call
# Reported in tokens, not dollars — a plan is decremented in tokens, and
# the provider's USD field is an estimate against a rate card we do not
# control (🎯T394).
#
#	make spend			# last 24h, every harness
#	make spend HARNESS=grok SINCE=... UNTIL=...
#	make spend-baseline		# the frozen 🎯T392 reference window
.PHONY: spend spend-baseline
spend:
	@go run ./cmd/harness-usage -spend \
	  $(if $(HARNESS),-harness $(HARNESS)) \
	  -since $(if $(SINCE),$(SINCE),$(shell date -u -v-24H +%Y-%m-%dT%H:%M:%SZ)) \
	  $(if $(UNTIL),-until $(UNTIL))

spend-baseline:
	@go run ./cmd/harness-usage -spend -harness grok \
	  -since 2026-08-08T01:53:00Z -until 2026-08-09T11:53:00Z

# ── Standing invariants (bullseye) ──────────────────
.PHONY: bullseye
bullseye:
	@go build ./... && echo "✓ build"
	@go test ./... && echo "✓ tests"
	@go vet ./... && echo "✓ vet"
	@test -z "$$(git status --porcelain)" && echo "✓ clean" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)
