# Self-test sites (🎯T110 / T110.4)

Self-test packs run against a **site class**. One report schema
(`schema_version`, `site`, `pack`, timestamps, `grade`,
`measurements[]`, optional `narrative`) is shared by every site.

| Site | Meaning | Default packs |
|------|---------|----------------|
| **live** | Daily owner surface (`:13705` / `~/.jevons`) | Soft L0–L2 only (health, layout policy, agents lineage). No destructive or chat-spamming packs. |
| **drill** | Non-prod isolate for aggressive packs | Future L3+ / layout chaos / fleet thrash. Must not default to daily state. |
| **ci** | Unattended hermetic / headless | Same soft packs via in-process hooks; no browser required for server-side packs. |

## Kick paths

```bash
# HTTP (same-origin or loopback)
curl -sS -X POST http://127.0.0.1:13705/api/self_test/run \
  -H 'Content-Type: application/json' \
  -d '{"pack":"all","site":"live"}'

curl -sS http://127.0.0.1:13705/api/self_test/packs

# MCP tools on the jevons MCP server
#   self_test.run  { pack?, site? }
#   self_test.list
```

`pack` may be a single id (`health-L1`, `composer-growth-L2`,
`agents-parent-L1`) or `all`. Grade is **always** derived from
`measurements[].ok` — narrative is commentary only.

## First packs (🎯T110.3)

| Pack | Class | What it grades |
|------|-------|----------------|
| `health-L1` | L1 | `GET /health` (or in-process equivalent) status ok |
| `composer-growth-L2` | L2 | Pure layout policy (28vh composer cap / growth-without-cover). DOM probe: `window.JevonsProbe.snapshot()` |
| `agents-parent-L1` | L1 | `GET /api/agents` parent lineage field exposure |

Web probe surface (🎯T110.1): `web/scripts/layout_probe.js` exports
pure helpers; the browser binds `window.JevonsProbe.snapshot()` with
composer height, messages viewport height, last-reply visible ratio,
and near-bottom.

## Drill bootstrap (phase-1 stub)

Phase 1 does **not** ship a full Drill UI tab. Aggressive packs that
need a throwaway daemon must use an **isolate**, not daily `:13705`.

### Ensure-isolate path (reuse later)

Prefer the existing journey-suite isolate (Universe B):

```bash
# From repo root — throwaway port + state dir + MCP name.
# Does NOT attach to daily :13705 / ~/.jevons.
make test-journey
# or:
go run ./scripts/journey-suite
```

The journey suite refuses the daily port by design. For a long-lived
drill daemon (future), start `bin/jevonsd` with an alternate state
directory and listen address (same flags the journey suite uses), then
kick packs with `"site":"drill"`.

### Rules

1. **live** never runs aggressive packs.
2. **drill** must not write into `~/.jevons` or bind `:13705` by default.
3. **ci** uses in-process hooks inside `go test` / daemon unit tests —
   no Playwright required for server-side packs.

## Residual (post phase-1)

- Full drill tab / ensure-isolate one-liner in the web UI
- Browser-backed packs that call `JevonsProbe.snapshot()` over CDP
- Pack allow-lists per site beyond soft L0–L2
