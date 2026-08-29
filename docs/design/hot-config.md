# Hot policy config (🎯T574)

**Rule:** no policy file under `~/.jevons` is read once at startup and
forgotten. Every file that governs behaviour registers with the one loader
seam — `config.Watcher` / `config.Watch[T]` in `internal/config/watch.go` —
and is re-read within `DefaultWatchInterval` (2 s; acceptance bar 5 s) of
its mtime or size moving.

## Semantics

- **Last-good stands.** A file that fails to parse never replaces the
  current value. The failure is logged with the path once per bad
  revision (not once per poll); the next good write heals it.
- **Missing is a value.** Deleting a file is a change back to the
  loader's defaults, not a silent hold — the jevons loaders all return
  defaults for `ENOENT`.
- **Poll, not fsnotify.** Editors write by rename, sync tools rewrite in
  place, and a missing file is legitimate; an mtime/size poll handles all
  three with one code path and no kqueue watch to lose across a rename.
- **Consumers call `Hot.Get()`** (or take an `OnChange`). Holding a
  copied value is the read-once bug in a new coat.

## Families

| File | Live fields | Restart-only |
|---|---|---|
| `capacity.json` | all — governor takes `policy.Get` | — |
| `budget.json` | limits, protected workers (overseer always pinned), accounting, soft caps | `disabled` (collector/enforcer existence) |
| `config.yaml` | `portfolios`, `providers` (via `ConfigManager.Reload`) | identity, bind/port, state paths, default provider/models, persona file |
| `llm-portfolio.json` | all — routing seed re-seeded | — |
| `rsi/coach_config.json` | all — the coach re-reads per cycle by design | — |
| owner MCP map | — | server set (see below) |

## Bounce-required elements self-bounce (🎯T392.5 path)

Owner 2026-08-29: "essentially everything should be in scope … in the
most extreme case, force a bounce." A change to a bounce-required element
is never ignored and never applied by hand: `bounceForConfig`
(`cmd/jevonsd/config_bounce.go`) names the elements to the owner and the
overseer (eventlog `config/restart_only_change`), then takes the 🎯T392.5
upgrade exit — SIGHUP to itself, so StopAll is skipped, reattach handles
are written and in-flight agent turns survive — and launchd KeepAlive
(🎯T553.3) re-raises the same binary, which reads the new file at boot.
Once per process. Only a supervised daemon is armed (development
`DailyPort` on the default state dir); an isolate has nothing to come
back under and only notifies. `JEVONS_CONFIG_BOUNCE=0` disarms for
batching edits. A malformed edit is last-good + warning, never a bounce.

### Bounce-required set (ratchet: shrinks by deliberate commit only)

| File | Element | Why no clean in-place rejig |
|---|---|---|
| `config.yaml` | `owner_name`, `overseer_name` | identity is baked into the overseer workdir, persona, and budget protection at boot |
| `config.yaml` | `bind_addr`, `port` | the listener and every stamped MCP URL (🎯T379) are bound at boot |
| `config.yaml` | `workdir`, `state_dir`, `sessions_dir`, `claude_projects`, `repos_root` | registries, stores, and scanners open on these paths at boot |
| `config.yaml` | `provider`, `model`, `overseer_model` | the overseer seat is minted with them (🎯T148) |
| `config.yaml` | `mcp_server_name`, `persona_file` | overseer MCP registration and persona template are rendered at boot |
| `budget.json` | `disabled` | decides whether the collector/enforcer exist at all |
| owner MCP map (`~/.claude.json`, `~/.grok/config.toml`, `~/.codex/config.toml`, `~/.cursor/mcp.json`; isolates `state_dir/mcp/*`) | `mcp owner map` — the server set only | the upstream proxy mounts on the HTTP mux at boot and every seat is minted with the map; a rewrite that leaves the servers alone (Claude Code's own state writes) is not a change |

Everything else in those files, and every other family, is hot.
`config.RestartOnlyDiff` is the code-side list; this table is the doc
side, and `scripts/docratchet` keeps them equal.

## Oracles

`internal/config/watch_test.go` (seam), `cmd/jevonsd/t574_hot_config_test.go`
(one test per family: write → swaps; garbage → last-good stands).
Owner-visible: edit `capacity.json`, `jevons_capacity_status` reflects it
within 5 s with no bounce.
