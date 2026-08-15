# Post-mortem: host RAM exhaustion via ghost Claude fleet (2026-08-15)

Status: incident review · Severity: high (host) · Author: investigation in
`~/think` session `b7fcc7d4-3de2-497b-a666-df4c4b11c054`, written up here
at the owner's request

## Summary

On 2026-08-15 the host (16-core, 128 GB) sat at load **267**, **0.2% idle**,
and **0.14 GB free**. The first reading was "CPU pegged." That was wrong.

Userland wanted only ~60% of the machine. The rest of the stall was the
kernel compressing and decompressing **~2 GB/s** (`compressor` 59.7 GB,
swap 45.9 / 47 GB). Threads were blocked on page faults, not computing.

The committed memory came from a **ghost Claude fleet**. Claudia's tmux
windows survive the consumer. Jevonsd restart had no way to adopt them
and `Registry.Stop` was a silent no-op without an in-memory handle, so
every bounce rematerialised the roster into **new** processes and left
the old ones running. One day's log held on the order of **500**
`agent started` events. Four counts, no reconciliation: ~24 agents in
the live list, 48 in `agents.json`, 46 tmux windows, 65 `claude`
processes.

A second, smaller leak: hermetic tests that spawn via `cmd/detach`
cleaned up only the process holding the port. Seven leftover `jevonsd`
stubs were found alive, two of them four days old.

A third, self-inflicted burst: 64 orphaned `zsh` CPU-burners from a
botched load-generator (`BURNERS=$(jobs -p)` is empty under `zsh -c`).
Those were killed first and were **not** the standing cause.

## Timeline (local time, 2026-08-15)

- **Morning / early investigation.** Owner screenshot of a drowning
  machine. 64 `ppid=1` `zsh` burners isolated to a leaked
  `TestOwnerHealthClearsFalseWorkingChrome` load-emulation script and
  killed. Load stays high.
- **Bitdefender thread.** `BDLDaemon` snapshots at ~54% of a core while
  the burners have load at 267. Exclusion list already covers `~/work`
  and `~/.mnemo`. CPU-time delta: **5.18 s / 30 s wall** (~17% of one
  core, ~1% of the machine). Dropped.
- **Interval accounting.** Sum of every userland process: 966% of
  1600% → **60.4% busy**. `top`: 61% user, 39% sys, **0.2% idle**,
  load **107**. ~630% of capacity is unattributed kernel work.
- **Memory, not CPU.**
  - compressions: 72,764/s (1,136 MB/s)
  - decompressions: 60,977/s (952 MB/s)
  - free 0.14 GB · compressor 59.71 GB · swap 45.9 / 47 GB
  - disk I/O near zero; no thermal throttle; AC power
- **Attribution.** ollama 19 MB idle (13 days). RSS undercounts
  (compressed/swapped pages). Approximate live RSS: Claude agents
  15.9 GB / 43 procs, MCP servers 8.4 GB / 433 procs, everything
  else filling the rest of a 2,268-process table.
- **Ghosts.** `jevonsd` pid 15567 with 44 tmux children;
  `claudia-anchor` creation timestamp moves 11:09:25 → 11:12:24
  (destroyed and rebuilt, not reattached). `Stop` only consults
  `r.procs`. `Start` always creates a new window. Grok connect-mode
  already had process reattach (🎯T40); the fleet runs Claude.
- **Containment (not a product fix).** Watchdog `bootout`, `jevonsd`
  stopped. Live Claude agents 65 → 23, then 3. Compression 1,136 →
  0 MB/s, free RAM 0.14 → 19 GB, load 267 → 20.5. Proof the thrash
  was fleet-driven.
- **Salvage.** Mapping of 41 registry rows written to
  `~/think/jevons-fleet-snapshot-2026-08-15.md` (`3ab4a2b`) before
  any further kills. Conversations live in
  `~/.claude/projects/*.jsonl`; the volatile piece was name →
  session → target → workdir.

## Why the first theories failed

1. **`top` %CPU is not a rate.** A 54% snapshot of `BDLDaemon` during
   overload was a symptom of the spawn treadmill, not the cause.
   CPU-time deltas contradicted it immediately and were ignored for
   too long.
2. **Load average is not "the CPUs are busy."** Load 107 with 60%
   userland CPU means runnable-or-uninterruptible threads, here
   waiting on the compressor.
3. **RSS is not committed memory** once the compressor and swap are
   in play. Adding the top processes never reached 128 GB; the
   missing mass was compressed.

## Root cause

Two missing properties, not one bug.

**Claudia has crash-survival without recovery.** The `Agent` doc
comment says the tmux substrate keeps the process alive if the
consumer dies. That is true. There was no `Adopt`. The only way to
obtain an `*Agent` was `Start`, which always creates a new window.
`Registry.Stop` / `StopAll` walked `r.procs`, which is empty after
every jevonsd restart (`save()` persists definitions, never
handles).

**Jevons treated every restart as rematerialise.** `StartAll` called
`Launch` for every `auto_start` row. Combined with the above, a
watchdog bounce or a SIGHUP-shaped restart doubled the fleet. The
upgrade path (🎯T40) already skipped `StopAll` and reattached Grok
serves by `ConnectURL`/`ConnectPID`. Claude had no analogue, and
Claude is what the fleet runs.

A process group would not have helped. Agent processes are children
of the claudia tmux server, and `cmd/detach` exists specifically so a
group sweep of the caller cannot reach them. On macOS a dead parent
reparents children to launchd; the group is not signalled.

## Contributing factors

- **Test-harness leaks.** `TestRestartThrashPolicy` (🎯T218) and the
  🎯T405/🎯T434 watchdog oracles detach a throwaway `jevonsd` and
  cleaned up with `lsof` on the scratch port. A daemon that lost the
  port survived as an init child. Not the RAM, but the same class:
  cleanup that cannot see what it spawned.
- **Blind admission.** `jevons_capacity_status` reported
  `pressure: normal, headroom 100%` at load 107 and 0.14 GB free
  (🎯T460 / 🎯T463). Nothing would have refused the next spawn.
- **SIGKILL as a verdict.** Three `exit=137` and three `[killed]`
  gate outputs the same day. A host-killed gate can read as pass or
  fail. Anything "completed" during the squeeze is untrusted
  (🎯T461).
- **Shared dirty tree.** 69 modified + 52 untracked files in jevons
  at the time of containment, plus a dirty claudia tree. Dozens of
  agents writing one working copy. Not a cause of the thrash; it
  made containment lossy.
- **False leads that cost time.** Bitdefender exclusions, ollama,
  Parallels. Each was measured and dropped.

## What we are not doing

**Do not reap on ordinary start.** A startup sweep would hide an
exit leak by construction. If exit leaks, the evidence should be
process count growing, solved out of band — not swept by the next
boot.

**Do not make adopt the default of `Launch`.** Adopt assumes leftover
processes are a normal input. That trains the system to tolerate a
leaky exit.

**Do not use process groups as the cleanup primitive.** See root
cause.

## Three models (the product contract)

| Intent | Signal | Exit | Next boot |
|---|---|---|---|
| **Drain** | `SIGTERM` | `StopAll` kills every agent this process started | `StartAll` → `Launch` creates. Leftovers are not reaped |
| **Start** | cold / crash | (cleanup never ran) | same as drain boot; extras stay visible |
| **Upgrade** | `SIGHUP` / `JEVONS_UPGRADE_EXIT=1` | skip `StopAll`, write `upgrade-handles.json` (now includes `TmuxWindowID`) | `StartAllPreferAdopt` reuses a live window; `Launch` only if that session actually exited. Handoff is then consumed |

Upgrade is the only special case. It already existed for Grok (🎯T40).
Claude is 🎯T40.1.

## What landed

Claudia (library the fleet actually runs):

- `d8413d3` / `bb13524` / `e83c48d` — 🎯T34. `Stop` kills a session
  window even with an empty handle map. `Adopt` / `ErrNoSessionWindow`
  / `StartAllPreferAdopt`. Ordinary `Launch` does **not** reap.
  Isolated-tmux oracles: leftover+`Launch` = 2 windows; `Adopt`
  keeps the same window id; `StartAll`+`StopAll` leaves zero of what
  this process started.

Jevons:

- `d477f98` — t218 / t434 / t405 sweep detached test daemons by
  executable path under the TempDir and fail the test on a survivor.
- `23e6dc9` / `1954543` — 🎯T40.1. Upgrade handoff carries
  `TmuxWindowID`. Boot with a handoff adopts; boot without one just
  `Launch`-es. Drain exit drops a stale handoff.
- Local `replace github.com/marcelocantos/claudia => ../claudia`
  until that claudia is published (🎯T448 is the standing pin rule).

Containment on the host (not in git): watchdog unloaded, `jevonsd`
left down, leftover fleet panes killed after the mapping snapshot.
iTerm Claude tabs were left alone.

## Still open

- **🎯T460 / 🎯T463** — admission must read the dimension that actually
  kills the host. A governor that reports 100% headroom at load 304
  certifies the outage it exists to prevent. In-flight dirty-tree
  work on `internal/capacity` and `internal/hostload` is not this
  write-up's claim.
- **🎯T459** — a fleet pane the registry does not know about is
  reaped, as a standing janitor, not as boot cleanup.
- **🎯T461** — SIGKILL is its own gate verdict.
- **Rebuild before the next daily start.** The installed `jevonsd`
  still links the old claudia pin. None of T34 / T40.1 is in that
  binary until it is rebuilt against `../claudia`.
- **Gates run during the squeeze.** Three `[killed]` outputs. Do not
  treat that window's greens or reds as done.

## Verification evidence (this incident)

- Compressor rate and memory breakdown measured over a 30 s interval,
  not a `top` snapshot.
- Machine recovered when the fleet was stopped and nothing else was
  changed (Bitdefender, Parallels, ollama untouched): compression
  1,136 → 0 MB/s, free 0.14 → 19.28 GB, idle 0.2% → 63.8%.
- `Stop` no-op and `Start`-always-creates read from
  `claudia/registry.go` and `claudia/agent.go` as they were that
  morning; the adopt/drain split is the subsequent commits above.
- Test leaks: `ps` found `/var/folders/.../T/TestRestartThrashPolicy*`
  and `TestTheInstalledAgentPATHRestartsFromAColdBin*` `jevonsd`
  binaries with `ppid=1` and zero children. After the path-sweep
  cleanup, both oracles pass with zero survivors.

## Related

- Earlier, different incident, same substrate:
  [postmortem-token-runaway-2026-07-06.md](postmortem-token-runaway-2026-07-06.md)
  (spend, not RAM). Detached tmux fleet was already invisible; this
  time the cost was the host, not the bill.
- Design: [upgrade-without-drain.md](../design/upgrade-without-drain.md) (🎯T40).
- Fleet mapping at containment:
  `~/think/jevons-fleet-snapshot-2026-08-15.md`.
