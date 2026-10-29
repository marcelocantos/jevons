BUILD_DIR := build

# ── Go binaries ─────────────────────────────────────
VERSION  ?= dev
LDFLAGS  := -ldflags "-X github.com/marcelocantos/jevons/internal/cli.Version=$(VERSION)"
GO_SRC   := $(shell find cmd internal -name '*.go' 2>/dev/null)
EMBED_GUIDE := internal/cli/help_agent.md

$(EMBED_GUIDE): agents-guide.md
	cp $< $@

.PHONY: all
all: jevonsd jevons-head treeguard commitscope commitbase fleetgit attrib runlock buildsnap recover detach jevons-watchdog bin/gate gotest mcpscope claudiapin buildident supervisorpolicy

# 🎯T710: unbuildable strays (bin/jevond predating the jevon-to-jevons
# rename) must not survive a clean. bin/ is gitignored; this recipe is
# what accounts for them. There is no Makefile product named jevond.
.PHONY: clean
clean:
	rm -rf bin

.PHONY: jevonsd
jevonsd: bin/jevonsd

bin/jevonsd: $(GO_SRC) $(EMBED_GUIDE) ui-build
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

# Shared-checkout git wrapper (🎯T1134). Refuses `git stash` in the shared
# clone from a fleet seat; prepend bin/ ahead of real git on PATH.
.PHONY: fleetgit
fleetgit: bin/fleetgit

bin/fleetgit: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/fleetgit ./cmd/fleetgit

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

# Restart serialiser (🎯T392.5). restart-jevonsd re-execs itself under
# this, so a missing binary means concurrent restarts race — which is how
# the daemon was left down on 2026-08-09. Built by `make all`, and the
# script fails closed rather than restarting unserialised.
.PHONY: runlock
runlock: bin/runlock

bin/runlock: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/runlock ./cmd/runlock

# supervisord restart-policy reader (🎯T1086). restart-jevonsd reads the
# program's autorestart before its upgrade SIGHUP. A missing binary is
# built on demand; an unbuildable one leaves the policy "unknown", and the
# script still starts a program supervisord leaves EXITED.
.PHONY: supervisorpolicy
supervisorpolicy: bin/supervisorpolicy

bin/supervisorpolicy: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/supervisorpolicy ./cmd/supervisorpolicy

# Detached diagnostician (🎯T415.1). jevonsd launches this setsid'd when
# convergence gives up on an agent, so it outlives the daemon and can even
# restart it. A missing binary skips diagnosis only — the deterministic
# owner notice does not depend on it.
.PHONY: recover
recover: bin/recover

bin/recover: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/recover ./cmd/recover

# Self-detach helper (🎯T405). restart-jevonsd re-execs itself through
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
# Owner gate (🎯T1279): Jevons ships to master by gated push, not by PR.
# Runs the product net on a commit in a clean checkout under bin/gate and,
# only on GREEN, writes .git/gate-attestation for the pre-push hook.
# `make gate` runs test-fast (every layer, every journey but the live-fleet
# ones); `make gate-full` runs test (🎯T1299). GATE_COMMIT=<sha> measures
# another commit; GATE_PROVIDER picks the journey backend (default
# openai-codex).
.PHONY: gate gate-full
gate: bin/gate
	@scripts/gate-attest.sh fast
gate-full: bin/gate
	@scripts/gate-attest.sh full

bin/gate: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/gate ./cmd/gate

# Achieve-time attestation check (🎯T765.1). Reads the proposed attestation on
# stdin; exit 0 verified/ungated/marked, 4 refused, 70 cannot judge. Contract:
# docs/design/achieve-verify-seam.md. Example:
#   printf '%s' "$ATT" | make -s achieve-verify LEDGER=path/bullseye.yaml ID=T765
.PHONY: achieve-verify test-achieve-verify
achieve-verify: bin/gate
	@test -n "$(LEDGER)" && test -n "$(ID)" || { echo "usage: make achieve-verify LEDGER=<path> ID=<Tn> < attestation" >&2; exit 2; }
	@bin/gate check-attestation -ledger "$(LEDGER)" -id "$(ID)"

# Hermetic oracle for the check, run under the gate so the status survives.
test-achieve-verify: bin/gate
	bin/gate -name achieve-verify -- go test -count=1 -run 'CheckAttestation|T765|T703' ./internal/gate

# 🎯T765: check-ledger walks live ledger attestations (audit equivalent of
# the finish-report FALSE-GREEN banner). Same package oracle as achieve-verify.
.PHONY: test-check-ledger
test-check-ledger: bin/gate
	bin/gate -name check-ledger -- go test -count=1 -run 'CheckLedger' ./internal/gate

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

# Development-daemon watchdog (🎯T405): one probe per invocation, run by a
# supervisor or scheduler on an interval; it restarts the development
# daemon when a bounce did not take and tells the owner. The daemon's own
# process is held by supervisord (`make supervisor-install`, 🎯T1111);
# launchd is not involved.
.PHONY: jevons-watchdog
jevons-watchdog: bin/jevons-watchdog

bin/jevons-watchdog: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/jevons-watchdog ./cmd/jevons-watchdog

.PHONY: watchdog-status
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

# 🎯T254.2: builds the development daemon from committed HEAD in a throwaway
# worktree, so one worker's uncommitted edits cannot stop another rebuilding.
.PHONY: buildsnap
buildsnap: bin/buildsnap

bin/buildsnap: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/buildsnap ./cmd/buildsnap

# 🎯T580: source identity of a build — this repo's HEAD/dirty plus every
# go.work sibling's — so the restart can tell "already activated" from a
# build whose only change came from claudia.
.PHONY: buildident
buildident: bin/buildident

bin/buildident: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/buildident ./cmd/buildident

# 🎯T448: daily-path claudia pin check — names pin SHA + sibling commits missing.
.PHONY: claudiapin
claudiapin: bin/claudiapin

bin/claudiapin: $(GO_SRC)
	@mkdir -p bin
	go build -o bin/claudiapin ./cmd/claudiapin

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
	@command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1 || { echo "ERROR: Node.js 24+ and npm are required to build React"; exit 1; }
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
# How many cores the test suites may take. This machine has 16 and the
# suite will happily use all of them -- docratchet alone builds whole
# repos in parallel worktrees and was measured at ~1286% CPU, which makes
# the machine unusable for whoever is sitting at it. Leave some behind.
# Override per-run: make test-go TEST_CPUS=16. Fleet seats running bin/gate
# -- go test … read test_max_procs from state_dir/capacity.json (🎯T781).
TEST_CPUS ?= 8
export GOMAXPROCS = $(TEST_CPUS)

# GOMAXPROCS alone is not enough for the suites that SHELL OUT to builds.
# It caps threads inside one process, but docratchet runs `go build` in
# throwaway worktrees, and each of those children is its own scheduler --
# so N test binaries x a full build each still saturates the box. -p caps
# how many package binaries run at once, which is the multiplier that
# actually hurts. Kept well under TEST_CPUS for that reason.
TEST_PKG_PAR ?= 4

# Per-package `go test` timeout (🎯T808). Go's default is 10m, and
# scripts/docratchet alone measured 664 s and 687 s (gates 24c6be1b,
# c155758f, clean @74dd18e9c, load 120-150) -- over the default, so a
# bare `go test ./...` kills it with a timeout panic. Its slow tests are
# deliberate-load and clean-checkout-build oracles whose cost IS the
# property (T450 thrash, T438/T398/T360 fresh builds), so the budget is
# raised rather than the tests slimmed. 45m = ~4x the measured runtime,
# and matches the docratchet gates already run with -timeout 45m.
GO_TEST_TIMEOUT ?= 45m

# Packages held race-clean (🎯T939). mcpserver is the concurrency hub (send
# queue drain, idle-nudge loop, restart-brief retry, re-attach watchers) and
# regressed under -race while test-go stayed green, because nothing ran it.
# Add a package here once `go test -race` over it is clean.
RACE_PKGS ?= ./internal/mcpserver

.PHONY: test test-go test-go-raw test-go-race test-web test-ui
# Hermetic Go tests never reach a claudia daemon installed on this machine:
# with one reachable, every Registry launch in a fixture would be granted a
# real seat with a real provider process behind it.
test-go test-go-raw test-go-race: export CLAUDIA_NO_BROKER = 1
test-go: bin/gotest
	@bin/gotest -timeout $(GO_TEST_TIMEOUT) -p $(TEST_PKG_PAR) ./...
	@bin/gotest -race -timeout $(GO_TEST_TIMEOUT) $(RACE_PKGS)

# The race step alone, when that is the thing being fixed.
test-go-race: bin/gotest
	@bin/gotest -race -timeout $(GO_TEST_TIMEOUT) $(RACE_PKGS)

# Escape hatch when the transcript itself is what you need.
test-go-raw:
	go test -timeout $(GO_TEST_TIMEOUT) ./...

# 🎯T553.1: seeded-throw oracle. Seeds an uncommitted deliberate throw into an
# ISOLATED clone (never the shared clone or :13705), builds it the daily way
# (buildsnap of committed HEAD) and the working-tree way (control), and scans
# what each serves. Two npm ci + vite + go builds, so it is not in test-go:
# the pure decision logic is (internal/t553oracle), the end-to-end run is here.
.PHONY: test-t553-oracle
test-t553-oracle:
	JEVONS_T553_ORACLE=1 CLAUDIA_NO_BROKER=1 go test -count=1 -timeout 40m -v -run TestT553SeededThrowNotExecutedByDaily ./internal/t553oracle

# 🎯T505: read-only probe of the development daemon itself. GET / and every
# asset it references must be byte-identical to ui/bundle.zip at the commit
# restart-jevonsd.sh last recorded as served (~/.jevons/restart-jevonsd.served).
.PHONY: test-t505-live
test-t505-live:
	JEVONS_T505_LIVE=1 go test -count=1 -v -run TestT505DevelopmentServesCommittedBundle ./internal/t553oracle

# React cockpit (🎯T540). The daemon embeds the tracked bundle (T540.2).
.PHONY: ui-dev ui-build test-ui-react ui-deps ui-daemon-install ui-daemon-stop ui-daemon-status
ui-dev:
	cd ui && npm run dev

# Canonical product bundle: TypeScript checking is part of acceptance (🎯T624).
# `make ui-dev` is the fast HMR path; Vitest alone does not type-check.
ui-build: ui-deps
	cd ui && npm run build
	go run ./scripts/package-ui

# Acceptance checks the tracked embed input before anything regenerates it.
.PHONY: ui-check-bundle
ui-check-bundle: ui-deps
	cd ui && npm run build
	go run ./scripts/package-ui -check

# Daily/dev servers (🎯T540.4 / vellum supervisor/ shape): tracked
# supervisor/*.ini with @REPO@, rendered into Homebrew supervisor.d.
# Vite :5173 is not a standing program — make ui-dev is opt-in HMR.
# supervisord is the only supervisor of the development daemon (🎯T1111).
.PHONY: supervisor-install supervisor-status
supervisor-install: bin/jevonsd
	supervisor/install.sh

supervisor-status:
	-supervisorctl status jevonsd

# ui/node_modules is gitignored, so a clean checkout of HEAD (bin/gate -clean,
# CI, a fresh clone) has no vitest until this install runs. 🎯T563: the old
# "skip when absent" branch returned 0 in ~100ms and let gate -clean print
# GREEN over a suite that never ran. Install when absent; never skip-and-green.
UI_VITEST_MOD := ui/node_modules/vitest

.PHONY: ui-deps
ui-deps: $(UI_VITEST_MOD)

$(UI_VITEST_MOD): ui/package.json ui/package-lock.json
	cd ui && npm ci
	@test -d $@ || (echo "npm ci did not install vitest at $@" >&2; exit 1)

test-ui-react: ui-deps
	cd ui && npm test

# 🎯T863: thin Lean oracle (lake build + zero sorry in lean/Invariants).
.PHONY: test-lean-invariants
test-lean-invariants:
	./scripts/lean-oracle.sh check

# Historical command retained as a React-unit alias. Legacy assertions are
# traceable in docs/audits/react-retirement-2026-09-05/legacy-suites.json.
test-web: test-ui-react

# 🎯T659: the sanctioned clean-checkout web gate (🎯T398). One recipe owns
# the worktree: it checks SHA (default HEAD) out detached, runs `make
# test-web` there under bin/gate — which installs ui deps inside that tree
# with npm ci, never through a link into this clone — and removes the tree
# behind gate.RemoveWorktree's foreign-symlink guard. The shared
# ui/node_modules is hashed before and after; a difference fails the run
# even when the suite was green. Do not hand-roll the worktree with
# ui/node_modules symlinked here: on 2026-09-15 that link let npm ci empty
# the shared install twice in one slice.
.PHONY: test-web-clean
test-web-clean:
	go run ./scripts/test-web-clean -sha $(or $(SHA),HEAD)

# Playwright perceptual chat UI (hermetic mocked WS). node_modules under
# scripts/browser-loop-test is gitignored (🎯T438), so a clean checkout of
# HEAD has no playwright until this install runs — same family as 🎯T360 /
# 🎯T398. Prefer not vendoring; make installs when absent.
PLAYWRIGHT_MOD := scripts/browser-loop-test/node_modules/playwright

.PHONY: playwright-deps
playwright-deps: $(PLAYWRIGHT_MOD)

$(PLAYWRIGHT_MOD): scripts/browser-loop-test/package.json scripts/browser-loop-test/package-lock.json
	cd scripts/browser-loop-test && npm ci
	@test -d $@ || (echo "npm ci did not install playwright at $@" >&2; exit 1)

# Browser checks drive the built React main/sidebar widgets. Transport is
# mocked here; the real packaged owner turn is journey J30.
.PHONY: test-ui playwright-browser t404-open-questions-ui-test
playwright-browser: playwright-deps
	cd scripts/browser-loop-test && npx playwright install chromium

t404-open-questions-ui-test: ui-build playwright-browser
	node scripts/react-ui-test/t404-open-questions-test.cjs

.PHONY: t1208-diagram-sanitization-ui-test
t1208-diagram-sanitization-ui-test: ui-check-bundle playwright-browser
	node scripts/react-ui-test/t1208-diagram-sanitization-test.cjs

test-ui: ui-build playwright-browser
	$(MAKE) test-ui-run

# test-ui without its build prerequisites, for testpar (🎯T1299): ui-build is
# phony, and two layers rebuilding ui/dist at once would race.
.PHONY: test-ui-run
test-ui-run:
	node scripts/react-ui-test/legacy-obligations.cjs
	node scripts/react-ui-test/test.cjs
	node scripts/react-ui-test/t789-send-cutin-test.cjs
	node scripts/react-ui-test/t799-composer-grow-test.cjs
	node scripts/react-ui-test/t949-resize-loop-test.cjs
	node scripts/react-ui-test/t532-idle-tab-test.cjs
	JEVONS_T1224_BROWSER=1 go test ./internal/server -run ^TestT1224BrowserTransport$$ -count=1
	node scripts/react-ui-test/t1126-reattach-test.cjs
	node scripts/react-ui-test/t1164-reload-pin-test.cjs
	node scripts/react-ui-test/t1208-diagram-sanitization-test.cjs
	node --test scripts/react-ui-test/boundary-oracle.test.cjs scripts/react-ui-test/boundary-prompt.test.cjs
	node scripts/react-ui-test/t404-open-questions-test.cjs

# 🎯T1274: real React viewport geometry on phone, iPad and desktop.
.PHONY: t1274-phone-ticker-test
t1274-phone-ticker-test: ui-build playwright-browser
	node scripts/react-ui-test/t1274-phone-ticker-test.cjs

.PHONY: test-ui-live
test-ui-live: playwright-browser
	@test -n "$(UI_HOST)" || (echo "Set UI_HOST to an isolated daemon host:port; use make test-journey for managed isolation" >&2; exit 2)
	node scripts/react-ui-test/test.cjs --host "$(UI_HOST)"

# Does the cockpit tell the truth (🎯T603)? Compares what the UI paints
# against /api/agents + /api/plan-usage AND against the live tmux server —
# three independent sources — and fails when they disagree. Written after
# the fleet panel showed seven agents "running" while tmux held none at
# all (🎯T602): every unit suite was green throughout, because no hermetic
# test can see a registry that has drifted from reality.
# Attaches to a RUNNING daemon; exits 2 (OUTAGE) when there is none.
.PHONY: test-cockpit-truth
test-cockpit-truth: playwright-deps
	node scripts/cockpit-truth/check.cjs

# Live scenario suite (🎯T51): drives a RUNNING jevonsd through the
# owner flows. Deterministic tier only by default; see scripts/live-suite
# flags for the overseer/spawn/restart/rewind scenarios.
# WARNING: defaults to your daily :13705 / ~/.jevons — prefer test-journey.
.PHONY: test-live-suite
test-live-suite:
	go run ./scripts/live-suite -skip-overseer

# Isolated owner-chat user journeys (separate port + state dir + MCP name).
# Does NOT touch the development stream. Part of `make test` (🎯T492): needing
# a signed-in provider CLI is a dependency of the suite, not a reason to
# omit the owner-visible net. Missing provider is OUTAGE (exit 2), not skip.
#
# PROVIDER selects the backend for the whole isolate — overseer and every
# agent the journeys spawn (🎯T282), e.g.:
#	make test-journey PROVIDER=claude
#	make test-journey ONLY=J32   # one journey (🎯T789.1)
# PORT is the isolate's listen port. The default 0 asks the OS for a free
# ephemeral port, so journeys started by different workers at once do not
# collide on :13715 (🎯T817). Pin one with PORT=13715; the suite still refuses
# the development ports (🎯T526 portguard).
PORT ?= 0
JOURNEY_SUITE = go run ./scripts/journey-suite -port $(PORT) $(if $(PROVIDER),-provider $(PROVIDER))
.PHONY: test-journey
test-journey: jevonsd playwright-browser
	$(JOURNEY_SUITE) $(if $(ONLY),-only $(ONLY))

# ── Gate speed (🎯T1299) ──────────────────────────────
# The live-fleet journeys run real coding workers end to end: J51 workers
# landing through integrate, J50 one worker (2-25 min). They are the full
# gate's, not every landing's: `make gate` runs test-fast, `make gate-full`
# runs test. (J39 took 10 min waiting on real 30 s fleet passes; with the
# isolate cadence knob it takes about 1 min and stays in the fast net.)
SLOW_JOURNEYS ?= J50-,J51-

# Layers without build prerequisites, for testpar. Each starts its own
# isolate on an ephemeral port (PORT=0), so the two suites can run at once.
.PHONY: test-prereqs test-journey-fast-run test-journey-slow-run test-fast
test-prereqs: ui-build bin/jevonsd bin/gotest playwright-browser
test-journey-fast-run:
	$(JOURNEY_SUITE) -skip $(SLOW_JOURNEYS)
test-journey-slow-run:
	$(JOURNEY_SUITE) -only $(SLOW_JOURNEYS)

# 🎯T1302: an instance has one overseer and one owner conversation, so the
# journeys that drive it run one at a time within an isolate. JOURNEY_SHARDS
# suites, each with its own isolate and overseer, split them by measured
# time (journey-suite -shard i/N); together they are test-journey-fast-run.
JOURNEY_SHARDS ?= 3
JOURNEY_SHARD_TARGETS := $(foreach i,$(shell seq 1 $(JOURNEY_SHARDS)),test-journey-shard-$(i))
test-journey-shard-%:
	$(JOURNEY_SUITE) -skip $(SLOW_JOURNEYS) -shard $*/$(JOURNEY_SHARDS)

# Fast product net: every layer at once, the live-fleet journeys left to the
# full net. The prerequisites build once first; the layers then share nothing.
test-fast: ui-check-bundle
	$(MAKE) test-prereqs
	go run ./scripts/testpar test-go test-web test-ui-run $(JOURNEY_SHARD_TARGETS)

# Full product net (🎯T492): every layer and every journey, all at once.
test: ui-check-bundle
	$(MAKE) test-prereqs
	go run ./scripts/testpar test-go test-web test-ui-run $(JOURNEY_SHARD_TARGETS) test-journey-slow-run

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
# 🎯T63: jevonsd restart against a clean-tree binary that depends on the
# published claudia daemon. Requires `claudia broker serve` (or brew
# services) and one live Session backend. Builds from HEAD in a worktree
# so the dirty daily checkout is not the oracle.
.PHONY: t63-daemon-reclaim
t63-daemon-reclaim:
	@scripts/t63-daemon-reclaim.sh

.PHONY: bullseye
bullseye: bin/gate
	@go build ./... && echo "✓ build"
	@bin/gate -name bullseye-test -- go test -timeout $(GO_TEST_TIMEOUT) ./... && echo "✓ tests"
	@go vet ./... && echo "✓ vet"
	@dirty=$$(git status --porcelain | grep -vE 'bullseye\.yaml$$' || true); \
	if [ -z "$$dirty" ]; then echo "✓ working tree clean"; \
	else \
	  echo ""; \
	  echo "================================================================"; \
	  echo "⚠  DIRTY WORKING TREE"; \
	  echo ""; \
	  echo "Warning only — invariants still pass (exit 0)."; \
	  echo "Look at the files below before starting a new target."; \
	  echo "Leftover work from a different objective → park it in a commit first."; \
	  echo "This session's WIP on the recommended target → continue."; \
	  echo "================================================================"; \
	  echo "$$dirty"; \
	  echo "================================================================"; \
	  echo ""; \
	fi

# Publish the formula to the Homebrew tap for an existing release. Run after
# the release assets exist — tapper reads them to build the formula.
release-tap:
	./scripts/release-tap.sh $(TAG)

.PHONY: release-tap
