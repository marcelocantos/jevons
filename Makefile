BUILD_DIR := build

# ── Go binaries ─────────────────────────────────────
VERSION  ?= dev
LDFLAGS  := -ldflags "-X github.com/marcelocantos/jevons/internal/cli.Version=$(VERSION)"
GO_SRC   := $(shell find cmd internal -name '*.go' 2>/dev/null)
EMBED_GUIDE := internal/cli/help_agent.md

$(EMBED_GUIDE): agents-guide.md
	cp $< $@

.PHONY: all
all: jevonsd jevons-head treeguard commitscope commitbase attrib runlock buildsnap recover detach jevons-watchdog gate gotest turndepth mcpscope

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
# only saves that cost on the first commit after a fresh clone.
#
# --install also puts the hook into this clone, because git never populates
# .git/hooks from the tree: a guard that waits to be copied by hand is absent
# in exactly the fresh checkout that needs it. It leaves a pre-commit hook it
# did not write alone, and says so.
.PHONY: commitscope
commitscope: bin/commitscope
	@bin/commitscope --install

bin/commitscope: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/commitscope ./cmd/commitscope

# Blessed private-index commit recipe (🎯T432). Use when `git commit --only`
# cannot: a shared hot file still holds another worker's uncommitted hunks.
# Seeds from HEAD, stages only named paths/blobs, re-checks HEAD before
# commit-tree, refuses on staleness — update-ref CAS alone is not enough.
.PHONY: commitbase
commitbase: bin/commitbase

bin/commitbase: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/commitbase ./cmd/commitbase

# Stopped-worker attribution (🎯T466). Operator lists / recovers / discards
# one agent's unfinished slice without transcript archaeology or a bulk
# checkout that destroys the other N-1. Built by `make all` so a fresh
# clone has `bin/attrib` after `make`.
.PHONY: attrib
attrib: bin/attrib

bin/attrib: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/attrib ./cmd/attrib

# Per-turn depth ceiling hook (🎯T392.4). The PreToolUse hook in
# .claude/settings.json execs this on every tool call, so `make all` builds
# it. A missing binary reports a visible non-blocking error and leaves the
# ceiling INACTIVE rather than refusing anybody's tool call — the shim
# reserves exit 2 for the checkpoint ask alone.
.PHONY: turndepth
turndepth: bin/turndepth

bin/turndepth: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/turndepth ./cmd/turndepth

# Restart serialiser (🎯T392.5). restart-daily-jevonsd re-execs itself under
# this, so a missing binary means concurrent restarts race — which is how
# the daemon was left down on 2026-08-09. Built by `make all`, and the
# script fails closed rather than restarting unserialised.
.PHONY: runlock
runlock: bin/runlock

bin/runlock: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/runlock ./cmd/runlock

# Detached diagnostician (🎯T415.1). jevonsd launches this setsid'd when
# convergence gives up on an agent, so it outlives the daemon and can even
# restart it. A missing binary skips diagnosis only — the deterministic
# owner notice does not depend on it.
.PHONY: recover
recover: bin/recover

bin/recover: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/recover ./cmd/recover

# Self-detach helper (🎯T405). restart-daily-jevonsd re-execs itself through
# this into a fresh session, so the caller's death — including the agent that
# the restart's own kill is about to stop — cannot cancel the bounce. Built by
# `make all`, and the script fails closed rather than restarting attached.
.PHONY: detach
detach: bin/detach

bin/detach: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/detach ./cmd/detach

# Gate runner (🎯T386 / 🎯T396). Runs a gate as a process — no shell, no
# pipeline — and records the status the process itself exited with, so a
# worker can cite a green it actually got and a misreported background run can
# be read back in band (`bin/gate last`). Built by `make all` because the
# fleet brief tells every worker to run gates through it.
.PHONY: gate
gate: bin/gate

bin/gate: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/gate ./cmd/gate

# MCP scope diagnosis and repair (🎯T464). An agent whose jevons_* tools are
# absent cannot call an MCP tool to ask why, so the answer has to arrive
# through Bash: `bin/mcpscope diagnose` says whether the daemon is down or
# this working directory is simply out of scope. Built by `make all` because
# the fleet brief tells workers to run it before reporting an outage.
.PHONY: mcpscope
mcpscope: bin/mcpscope

bin/mcpscope: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/mcpscope ./cmd/mcpscope

# Daily-daemon supervisor (🎯T405). launchd runs this every 30s, outside every
# process tree a restart tears down, and calls the restart script when the port
# stays dead. `make watchdog-install` writes and loads the LaunchAgent.
.PHONY: jevons-watchdog
jevons-watchdog: bin/jevons-watchdog

bin/jevons-watchdog: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/jevons-watchdog ./cmd/jevons-watchdog

.PHONY: watchdog-install watchdog-uninstall watchdog-status
watchdog-install: bin/jevons-watchdog
	bin/jevons-watchdog -install

watchdog-uninstall: bin/jevons-watchdog
	bin/jevons-watchdog -uninstall

watchdog-status: bin/jevons-watchdog
	@bin/jevons-watchdog -status

# Test verdict harness (owner, 2026-08-11). Wraps `go test -json` and
# reports pass/fail with counts instead of a transcript, so a failing
# suite cannot hide in log noise and a piped exit code cannot be lost.
.PHONY: gotest
gotest: bin/gotest

bin/gotest: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/gotest ./cmd/gotest

# 🎯T254.2: builds the daily daemon from committed HEAD in a throwaway
# worktree, so one worker's uncommitted edits cannot stop another rebuilding.
.PHONY: buildsnap
buildsnap: bin/buildsnap

bin/buildsnap: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/buildsnap ./cmd/buildsnap

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
# test-go reports a VERDICT, not a transcript (cmd/gotest). Bare
# `go test ./...` emits thousands of legitimate log lines from passing
# tests, and its failure signal is a few uppercase words scattered
# through them — which a human scans for and an agent drowns in. Twice on
# 2026-08-10 a failing suite was recorded as green here, because
# `go test ./... | grep -v '^ok'` reports GREP's exit code, not the
# tests'. gotest keeps the exit code, counts what ran, treats zero tests
# and build failures as failures, and puts the transcript in a file.
.PHONY: test test-go test-go-raw test-web test-ui
test-go: bin/gotest
	@bin/gotest ./...

# Escape hatch when the transcript itself is what you need.
test-go-raw:
	go test ./...

# Hermetic Node tests for chat working-indicator lifecycle (🎯T39)
# and attention-thread model (🎯T65).
test-web:
	node web/scripts/boot_sentinel_test.js
	node web/scripts/module_gate_test.js
	node web/scripts/chat_events_test.js
	node web/scripts/owner_turn_shape_test.js
	node web/scripts/attention_threads_test.js
	node web/scripts/idea_capture_test.js
	node web/scripts/aside_history_test.js
	node web/scripts/fleet_row_test.js
	node web/scripts/fleet_paint_test.js
	node web/scripts/fleet_cycle_test.js
	node web/scripts/fleet_selection_test.js
	node web/scripts/model_prefix_test.js
	node web/scripts/provider_menu_test.js
	node web/scripts/portfolio_group_test.js
	node web/scripts/virtual_list_test.js
	node web/scripts/idle_monitor_test.js
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
	node web/scripts/viewport_census_test.js
	node web/scripts/frontier_table_test.js
	node web/scripts/rsi_dispositions_test.js
	node web/scripts/target_context_chrome_test.js
	node web/scripts/target_hotspot_test.js
	node web/scripts/mermaid_actions_test.js
	node web/scripts/markdown_normalize_test.js
	node web/scripts/decision_matrix_test.js
	node web/scripts/streaming_markdown_test.js
	node web/scripts/cost_display_test.js
	node web/scripts/plan_usage_test.js
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
	node scripts/chat-ui-test/t493-visibility-test.js
	node scripts/chat-ui-test/t363-scroll-up-anchor-test.js
	node scripts/chat-ui-test/t361-owner-ux-test.js
	node scripts/chat-ui-test/t309.1-conversation-widget-test.js
	node scripts/chat-ui-test/t340-frontier-table-layout-test.js
	node scripts/chat-ui-test/t390.1-plan-ticker-layout-test.js
	node scripts/chat-ui-test/t366-composer-tab-cycle-test.js
	node scripts/chat-ui-test/t374-no-onerror-test.js
	node scripts/chat-ui-test/t374-module-gate-test.js
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
# Does NOT touch daily-driver stream. Part of `make test` (🎯T492): needing
# a signed-in provider CLI is a dependency of the suite, not a reason to
# omit the owner-visible net. Missing provider is OUTAGE (exit 2), not skip.
#
# PROVIDER selects the backend for the whole isolate — overseer and every
# agent the journeys spawn (🎯T282), e.g.:
#	make test-journey PROVIDER=claude
.PHONY: test-journey
test-journey: jevonsd
	go run ./scripts/journey-suite $(if $(PROVIDER),-provider $(PROVIDER))

# Full product net (🎯T492): hermetic layers first, then Universe-B journeys.
test: test-go test-web test-ui test-journey

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
#
# The test step runs under bin/gate (🎯T386): the status recorded is go test's
# own, and a suite that exits zero while printing a timeout panic comes out
# SUSPECT rather than being echoed as a tick. The claudia incident that raised
# the target was this recipe's sibling running `go test -race ./... | tail -n 5
# && echo "✓ tests"`, which printed the tick over a failed suite. Cite the
# GATE line this prints, not the tick.
.PHONY: bullseye
bullseye: bin/gate
	@go build ./... && echo "✓ build"
	@bin/gate -name bullseye-test -- go test ./... && echo "✓ tests"
	@go vet ./... && echo "✓ vet"
	@test -z "$$(git status --porcelain)" && echo "✓ clean" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)
