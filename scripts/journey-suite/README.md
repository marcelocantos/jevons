# Journey suite (isolated owner-chat E2E)

**What a user journey is (owner 2026-08-01, 🎯T107):** a mapped **owner-visible
interaction with the product that runs end-to-end and must interact with an agent** (overseer and/or fleet worker). Grepping docs, pure helper unit tests,
and hermetic UI mocks are **not** journeys.

**Standing product doctrine (🎯T101 / 🎯T492):** this suite is the **preferred E2E net**
for owner-visible chat/fleet behaviour and is part of `make test`. Hermetic
Go/Node/Playwright mocks are **distinct from** journeys (no Grok, no isolate)
but they are not a substitute. Needing a signed-in provider CLI is a
dependency of `make test`, not a reason to omit this net. A missing
provider is OUTAGE (exit 2), not skip-and-green.

**Agent interaction:** each journey drives a real isolate (`jevonsd` + Grok ACP
or fleet tools that reach an agent). After a **successful** live run, caching
that agent interaction for replay is allowed (🎯T107) — not inventing stubs
that never hit an agent.

**Journey-or-exception:** when an owner-visible failure is fixed, add a
journey that covers it, **or** an explicit exception naming why
unit/hermetic coverage is enough.

## Two universes

Keep them distinct. Most agent/automated work lives in **B**.

| | **A — Daily driver** | **B — Journey / throwaway** |
|---|---|---|
| When | Real owner use; rare diagnosis that **needs** live context | **Preferred E2E** owner-chat + fleet journeys |
| Port | `:13705` | **13715** (or `-port 0`; **never** 13705) |
| State / chatlog | `~/.jevons` | `$TMPDIR/jevons-journey-*` |
| MCP | `jevonsmcp` | `jevonsmcp-journey` (removed on exit) |

**Policy:** do not drive Universe A unless the bug genuinely requires the
owner’s session, journal, or MCP surface. Prefer this suite. The suite
**refuses** port `13705` so journeys cannot silently bind the daily
driver. Scripts that still attach to a running daemon
(`make test-live-suite`, `chat-smoke`, `chat-smoke-cancel`) default to
`:13705` — use them on purpose, not as the routine path.

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

Needs: Grok CLI signed in (same as daily jevonsd). Part of `make test` (🎯T492).

Hermetic meta-checks (doc inventory, port guard) live outside this section:
`scripts/docratchet/` and `scripts/journey-suite/portguard/` — **not** journeys.
Those packages are **meta-guards only**, never called “journeys.”

## Step library (🎯T102)

Journeys stay plain Go. Shared setup/act/assert helpers live in `steps.go`
in this package (e.g. `ListAgentsHTTP`, `MCPToolCall`, `AgentStart`/`Send`,
`MustAgentRunning`). Prefer steps over copy-paste; no formal YAML/DSL.

## Journey cache (🎯T107)

A **successful** live run may be cached for later replay (store the
observed agent interaction after green; on cache hit, replay recorded
frames; on miss or invalidation, re-run live). Cache is optional and
must never turn a non-agent hermetic into a “journey.” Interface sketch:
record under `$JEVons_JOURNEY_CACHE` or suite state after PASS; invalidate
when journey source hash or jevonsd binary mtime changes. Not required for
every run today; live is the default truth.

## Journeys

### Owner chat
1. **J1-health** — `/health`
2. **J2-chat-round-trip** — idle send → terminal
3. **J3-cancel-and-send** — long turn → interrupt → settle → replacement → terminal
4. **J4-reconnect-sealed** — seed turn → reconnect → bounded replay + sandbox journal only
4b. **J19-root-history-paint** — seed ≥12 distinct sealed owner turns into the isolate journal (not the owner's history) → Playwright census of `__transcriptRows` plus T493 gates (`checkVisibility`, centre hit-test, Vision OCR of a pinned 1280×800 viewport). Empty pane with model rows is a fail (🎯T494).
4c. **J20-plan-dest** — fixture weekly remaining (not live vendor) → omit-provider mint refuses when dest empty; sweep parks an explicit-hot worker (🎯T390.1.5). Overseer chat turn proves isolate agent interaction.

### Orchestration (MCP-direct on the isolate)
5. **J6-mcp-tool-surface** — agent + thread tools registered
6. **J7-overseer-registry** — overseer running in `/api/agents` and `agent_list`
7. **J8-two-agents-same-workdir** — two fleet agents, same workdir, distinct sessions (T86 live)
8. **J8b-po-worker-lineage-fanout** — PO + worker parent lineage + T104 first-send brief
9. **J9-thread-spawn-direct** — spawn → direct short turn → remove
10. **J10-worker-shell-tool** — worker runs `run_terminal_command` (T97 permission regression); marker file oracle
11. **J6b-mcp-reconnect** — live `jevons_mcp_reconnect` (T60/T105.1)
11b. **J21-goal-continue-all-backends** — work mint on Claude, Grok, and Codex; after the first terminal the host starts a second user turn with no `jevons_agent_send` (🎯T510). Missing CLI is OUTAGE, not skip-and-green.

### Teardown oracle
12. **J5-isolation** — sandbox journal under temp state; journey MCP gone; daily MCP intact

## Cleanup

On exit the suite always stops the isolated daemon and runs
`grok mcp remove jevonsmcp-journey` so `~/.grok/config.toml` is not left
pointing at a dead test port. The daily `jevonsmcp` entry is never removed.
