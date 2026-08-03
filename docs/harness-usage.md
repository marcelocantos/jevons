# Harness usage reports (🎯T163)

On-demand usage reports for every coding harness in the Jevons fleet.
Observational only — live spend enforcement remains in `internal/cost`
(🎯T36).

## Supported harnesses

| Harness | Primary source | Fallback |
|---|---|---|
| **Claude Code** | Local JSONL under `~/.claude/projects/**/<session>.jsonl` (`message.usage`, optional `costUSD`) | Anthropic Admin Usage API (not wired; needs org admin key — residual) |
| **Grok / ACP** | Local `~/.grok/sessions/**/updates.jsonl` `turn_completed` (`costUsdTicks` preferred) | No public xAI usage rollup |
| **Codex** | Local `~/.codex/**/rollout-*.jsonl` `token_count` events + rate_limits | OpenAI org usage API (not wired — residual) |
| **Cursor** | Dashboard export JSON (`settings → Usage`) | `~/.cursor/ai-tracking/ai-code-tracking.db` (activity hashes, not full billing) |

Preference order per harness:

1. Official/API usage surfaces when credentials and endpoints exist.
2. Local harness logs / state (JSONL, SQLite).
3. Fixture-backed paths for hermetic tests and offline repros.

Auth and rate limits never hard-fail a report: they degrade to the local
scrape with a note on the report.

## CLI

```bash
# Live data under $HOME
go run ./cmd/harness-usage
go run ./cmd/harness-usage -harness grok
go run ./cmd/harness-usage -json

# Probe provider APIs (still falls back to local scrape)
go run ./cmd/harness-usage -try-live-api

# Hermetic fixtures (same trees as package tests)
go run ./cmd/harness-usage -json \
  -claude-root internal/harnessusage/testdata/claude \
  -grok-root   internal/harnessusage/testdata/grok \
  -codex-root  internal/harnessusage/testdata/codex \
  -cursor-dashboard internal/harnessusage/testdata/cursor/dashboard-usage.json
```

Flags:

| Flag | Purpose |
|---|---|
| `-harness` | `claude` \| `grok` \| `codex` \| `cursor` \| `all` (default) |
| `-json` | JSON array of `Report` objects |
| `-try-live-api` | Probe creds / document residual; never blocks scrape |
| `-claude-root` / `-grok-root` / `-codex-root` | Override data roots |
| `-cursor-dashboard` | Path to Cursor Usage export JSON |
| `-cursor-db` | Path to `ai-code-tracking.db` |
| `-since` | RFC3339 lower bound on event timestamps |
| `-max-files` | Cap session files walked (smoke on huge trees) |

## Library

```go
import "github.com/marcelocantos/jevons/internal/harnessusage"

reps := harnessusage.CollectAll(&harnessusage.CollectArgs{
    Roots: map[harnessusage.Harness]string{
        harnessusage.HarnessGrok: "/path/to/.grok",
    },
    CursorDashboardJSON: "export.json",
})
```

`Collect` / `CollectAll` accept `CollectArgs` with roots, home override,
Cursor paths, `Since`, `MaxFiles`, fixed `Now` (tests), and `TryLiveAPI`.

## Hermetic oracle

```bash
go test ./internal/harnessusage/ -count=1
```

Fixtures live under `internal/harnessusage/testdata/{claude,grok,codex,cursor}/`.
They cover token totals, cost ticks, Codex rate limits, Cursor dashboard
spend, and Cursor tracking-DB activity rollups. No network, no `$HOME`
harness state.

## Cursor dashboard export

Cursor has no public usage API. For billing-shaped numbers:

1. Open Cursor → Settings → Usage (or account usage page).
2. Save/export the cycle summary as JSON matching the fixture shape in
   `testdata/cursor/dashboard-usage.json` (`used_requests`,
   `on_demand_spend_usd`, per-model tokens).
3. Pass `-cursor-dashboard path.json`.

Without an export, the CLI falls back to `ai-code-tracking.db`, which
counts code-hash activity by model/conversation — useful for volume, not
USD.

## Residual (accepted)

- Provider auth and rate limits may block live API hits; fixture/local
  scrape always remains.
- Anthropic Admin Usage API and OpenAI organization usage API are not
  wired end-to-end (keys + date-range pagination + org scope). Notes
  document the gap when `-try-live-api` is set.
- Cursor billing requires a manual/dashboard export for spend accuracy.

## Related

- Live fleet spend clamp-down: `internal/cost` (🎯T36), historical note
  in [cost-management.md](cost-management.md).
- Package docs: `internal/harnessusage`.
