# Jevons

Coordinator for a community of coding agents under a single CEO agent.
Go daemon (`jevonsd`) + canonical web UI + iOS thin client. The only
agent provider is Grok, via the claudia harness.

**Architecture:** read [docs/architecture-current.md](docs/architecture-current.md)
— the one honest, current description of the system (components, agent
model, persistence, security posture, glossary). Do not trust older
design docs without checking their supersession banners.

## Build

```bash
make              # Build jevonsd
make init         # Install prerequisites (Go 1.26+, C compiler for CGo/SQLite)
make ios          # Regenerate the iOS Xcode project (xcodegen)
```

## Run

```bash
make run          # Build and run jevonsd (or: brew services start jevons)
open http://localhost:13705/   # Canonical web UI
```

Dev mode serves `web/` from disk with hot reload.

## Test

```bash
make test         # All: Go + web hermetic (Node) + Playwright UI (hermetic)
make test-go      # go test ./...
make test-web     # node web/scripts/chat_events_test.js
make test-ui      # Playwright perceptual chat UI (mocked WS)
make test-ui-live # Same, against a running jevonsd
make bullseye     # Standing invariants: build, test, vet, clean tree
```

## Code conventions

### Go
- **slog** for structured logging.
- `cmd/` for binaries, `internal/` for packages.
- Durable state: atomic write-and-rename; malformed state is a hard
  error, never a silent reset. No silent-fail sends/directs.

### Web
- `web/index.html` is self-contained; pure logic lives in
  `web/scripts/*.js` (DOM-free where possible so Node tests can require it).
- Server↔client chat events go through the normalization layer
  (`internal/server/chat_wire.go` + `web/scripts/chat_events.js`) — keep
  both sides in sync and covered by the hermetic tests.

### iOS
- Thin client only: logic belongs server-side; the app wraps the web UI
  in a WKWebView over the pigeon relay. Do not grow native UI.

### General
- Default branch is `master`.
- Keep concerns modular and orthogonal; platform-specific code in
  separate files.
- Convergence targets live in `bullseye.yaml` (🎯Tn); target lifecycle
  rides the PR that changes it.
- **Fleet spawn (🎯T78):** child implementation work uses Jevons fleet
  agents (`jevons_agent_start` / durable threads), **not** Grok
  `spawn_subagent` / worktree children that die with the parent and never
  show in the RHS fleet panel. Full doctrine: `internal/config/persona.md`
  and `agents-guide.md`.

## Project structure

```
jevons/
├── Makefile              # Build orchestration (Go-only since 🎯T43)
├── cmd/jevonsd/          # Coordinator daemon entry point
├── internal/             # server, butler, thread, fleet, mcpserver,
│                         # cost, auth, discovery, transcript, cli
├── web/                  # Canonical web UI (served by jevonsd)
├── ios/Jevon/            # iOS thin client (WKWebView + pigeon)
├── scripts/              # chat-smoke, chat-ui-test, browser-loop-test
├── docs/                 # charter, architecture-current, design docs
└── bullseye.yaml         # Intent ledger
```

## Delivery

Merged to `master` via squash-only PRs; releases via the `/release`
flow (Homebrew tap `marcelocantos/tap/jevons`, brew-services daemon).
