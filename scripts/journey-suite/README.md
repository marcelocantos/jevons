# Journey suite (isolated owner-chat E2E)

Small user-journey suite against a **throwaway** `jevonsd` — the path to
use once Jevons is the daily driver, so test traffic never lands in the
live owner stream.

| Isolation | Daily driver |
|---|---|
| Port **13715** (or `-port 0`; never 13705) | `:13705` |
| State under `$TMPDIR/jevons-journey-*` | `~/.jevons` |
| Chatlog in that sandbox | `~/.jevons/chatlog/…` |
| MCP name `jevonsmcp-journey` | `jevonsmcp` |

`make test-live-suite` still targets a *running* daemon (often daily) —
prefer this suite for owner-chat journeys.

## Run

```bash
make jevonsd          # once
make test-journey     # starts isolate → journeys → teardown
```

Options:

```bash
go run ./scripts/journey-suite -keep          # leave sandbox dir for debug
go run ./scripts/journey-suite -port 0        # ephemeral port
go run ./scripts/journey-suite -bin ./bin/jevonsd
```

Needs: Grok CLI signed in (same as daily jevonsd). Not part of default `make test`.

## Journeys

1. **J1-health** — `/health`
2. **J2-chat-round-trip** — idle send → terminal
3. **J3-cancel-and-send** — long turn → interrupt → settle → replacement → terminal
4. **J4-reconnect-sealed** — seed turn → reconnect → bounded replay + sandbox journal only
5. **J5-isolation** — sandbox journal under temp state; journey MCP gone; daily MCP intact

## Cleanup

On exit the suite always stops the isolated daemon and runs
`grok mcp remove jevonsmcp-journey` so `~/.grok/config.toml` is not left
pointing at a dead test port. The daily `jevonsmcp` entry is never removed.
