# Journey suite (isolated owner-chat E2E)

## Two universes

Keep them distinct. Most agent/automated work lives in **B**.

| | **A — Daily driver** | **B — Journey / throwaway** |
|---|---|---|
| When | Real owner use; rare diagnosis that **needs** live context | Default E2E owner-chat journeys and smoke |
| Port | `:13705` | **13715** (or `-port 0`; never 13705) |
| State / chatlog | `~/.jevons` | `$TMPDIR/jevons-journey-*` |
| MCP | `jevonsmcp` | `jevonsmcp-journey` (removed on exit) |

**Policy:** do not drive Universe A unless the bug genuinely requires the
owner’s session, journal, or MCP surface. Prefer this suite. Scripts that
still attach to a running daemon (`make test-live-suite`, `chat-smoke`,
`chat-smoke-cancel`) default to `:13705` — use them on purpose, not as the
routine path.

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
