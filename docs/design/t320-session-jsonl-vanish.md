# 🎯T320 — Session JSONL “vanish” across daemon restarts

**Status:** root cause identified (2026-08-08). Product fix is **not** in
jevonsd — it belongs in **claudia** (`Materialized` set at process spawn).
T313 remains the recovery net when loss already happened.

**Date:** 2026-08-08  
**Worker:** `jv-t320-jsonl-vanish` (Grok seat; prior Claude seat reminted)

---

## Verdict (acceptance 1)

| Branch | Result |
|--------|--------|
| **Deleted** (unlink on kill/restart) | **Refuted** |
| **Wrong-slug** (JSONL under another project dir) | **Refuted** for the lost ids |
| **Never-written** | **Confirmed** |

Session IDs were registered and processes started (term logs + chains exist),
but Claude Code never created `~/.claude/projects/<slug>/<id>.jsonl` because
**no turn was ever submitted**. On the next launch, claudia’s fail-closed
resume (`RequireResume` ← `Materialized`) refuses to mint a replacement —
which looks like “JSONL vanished,” but the file was never on disk.

---

## Named ids (acceptance list)

Slug for jevons workdir:  
`~/.claude/projects/-Users-marcelo-work-github-com-marcelocantos-jevons/`

| Session prefix | JSONL on disk? | Term log | What the term shows |
|----------------|----------------|----------|---------------------|
| `cd641cad…` | **no** | yes (~4 KB) | Multi-block **Pasted text** in composer; **no** assistant/result |
| `269603dd…` | **no** | yes (~4 KB) | Same — paste chips, never submitted |
| `f38d5f99…` | **no** | yes (~4 KB) | Same |
| `80e5ef75…` | **no** | yes (~4 KB) | Same |
| `1a34bc6c…` | **yes** (821 KB, 374 lines) | yes | Full conversation |
| `9f0beb4b…` | **yes** (528 KB, 206 lines) | yes | Full conversation |

**Survivors:** `1a34bc6c`, `9f0beb4b`.  
**Distinction:** both completed real turns (JSONL has `user` + `assistant`
events; first user ≈ first asst within ~2 s). Never-written seats show only
startup chrome + unsubmitted paste; zero assistant/tool stream in the term.

Wrong-slug check: for the four missing ids, no `*/<id>.jsonl` under any
`~/.claude/projects/*` path. Only term + chain under
`~/.local/state/claudia/`.

---

## How claudia launches Claude Code

From `claudia/agent.go` (`claudeAgentBackend.StartAgent`):

- **New session (JSONL absent):** `claude … --session-id <uuid>`
- **Resume (JSONL present):** `claude … --resume <uuid>`

`SessionJSONLPath` =  
`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`.

`Start` does **not** create that file. Claude Code writes it when a
conversation turn is actually run (pool comment: session/JSONL populated when
the consumer sends and Claude creates the transcript;
`TestAgentSendAndWaitForResponse` documents that a non-submit Enter leaves
prompt inserted, no API call, **no JSONL events**).

Registry contract (`claudia/registry.go`):

```go
// Materialized records that SessionID has hosted a real conversation
// (a successful launch). Once set, launches pass RequireResume …
RequireResume: def.Materialized,
// …
def.Materialized = true   // immediately after Start() returns OK
```

So “materialized” today means **process spawned**, not **conversation
persisted**. Comment and behaviour disagree with the fail-closed resume
guard that assumes a JSONL exists.

---

## Restart / kill / teardown (no deletion)

| Path | Touches Claude JSONL? |
|------|------------------------|
| `Registry.Stop` / `Remove` | Stop process / drop registry row only — **no** `os.Remove` on JSONL |
| `tmuxagent.KillWindow` | Kills tmux window only |
| `scripts/restart-daily-jevonsd.sh` | Rebuild, kill port listeners, start daemon — **no** session cleanup under `~/.claude` |

Survivors’ JSONLs remained across today’s restart storm (14:09, 14:24, 14:27,
14:37, 14:40, 14:45, 14:53…) — further evidence against teardown deletion.

---

## Causal chain (routine “loss” after restart)

1. `jevons_agent_start` / auto-start → `Registry.Launch` → Claude process up
   with `--session-id`.
2. `Materialized=true` persisted **before** any turn completes.
3. Brief delivery pastes large multi-block text into the TUI composer.
   On several lost seats, submit never lands (term frozen on
   `[Pasted text #N +… lines]`). Same class of bug claudia already fixed once
   for Send (`\n` = Shift+Enter vs CR = Enter).
4. No submitted turn → **Claude never writes JSONL**.
5. Daemon restart tears down tmux windows; term/chain remain; JSONL still
   absent.
6. Auto-start / resume: `RequireResume=true`, `SessionExists=false` → hard
   error: *“existing conversation required but JSONL not found … refusing to
   mint a replacement session”*.
7. Operator experience: “sessions vanished”; actually **never written**, then
   **locked out** by Materialized.

Timeline match (daily log): e.g. `cd641cad` / `269603dd` started ~10:44,
restart ~10:51 → auto-start JSONL-not-found; remint `f38d5f99` / `80e5ef75`
~10:53, restart ~10:59 → same. Seats that **did** complete turns
(`1a34bc6c`, `9f0beb4b`) kept JSONLs and resume without that refusal.

Still recurring after T313 for other Claude POs (same failure text for
`bullseye-po`, `claudia-po`, `yourworld2-po` at 14:53 / 14:59) — recovery
rotates after loss; it does not prevent never-written + Materialized lock.

---

## Two defects (both in **claudia**)

| | Defect | Severity |
|---|--------|----------|
| **(a)** | `Materialized` set on successful `Start()`, not when JSONL (or equivalent provider transcript) exists | Makes never-written permanent |
| **(b)** | First brief can leave composer unsubmitted (paste / keystroke) so no turn starts | Trigger that leaves no JSONL |

**(a)** alone would have avoided permanent lockout: next launch would mint
with `--session-id` again (empty history, but recoverable without kill).  
**(b)** is why some seats never get a transcript before the next restart.

T313 (`SessionLost` / `RehydrateLostSession`) is the intentional recovery when
Materialized + missing JSONL is already on disk in the registry. It does not
fix (a).

---

## Recommended product fix (claudia; cross-repo)

**Primary (a) — set Materialized only when a conversation is resumable:**

- After `Start()`, leave `Materialized=false` until `SessionExists` (Claude
  JSONL present) or an equivalent first-turn signal.
- Or: set Materialized only after first JSONL line / first completed turn
  event is observed (tail callback / `WaitForResponse` path).
- Hermetic: `Launch` → kill before Send → registry `Materialized=false` and
  relaunch does **not** pass `RequireResume`.
- Hermetic: `Launch` → WriteFile JSONL (or live Send) → then Materialized
  may become true and RequireResume applies.

**Secondary (b) — ensure first-turn flush before treating seat as durable:**

- Harden Send submit for large paste briefs (already partially addressed
  historically for Enter vs Shift+Enter).
- Optional host policy: do not mark auto-start “healthy” until JSONL exists
  or first turn completes (still prefer fixing Materialized in claudia so
  all hosts benefit).

**Out of scope for jevons-only:** unlinking is not the bug; restart script
should not invent session cleanup. Jevons T313 stays as residual safety net
(acceptance residual).

---

## Scripted repro sketch (for claudia / journey; acceptance 3)

```text
1. Register Claude agent, Launch (Materialized becomes true today).
2. Confirm SessionExists == false (no JSONL yet).
3. Kill process / simulate restart without Send.
4. Launch again → today: hard error; after fix: mint or resume only if JSONL exists.
5. Control: Launch → Send → wait JSONL → restart → resume succeeds, same session id.
```

Daily-path (acceptance 4): after claudia fix is released into the daemon’s
module path, detached `restart-daily-jevonsd` with pre-restart-alive Claude
workers that **completed ≥1 turn** should show zero
`JSONL not found` / rehydrate-on-missing-JSONL for those agents. Seats killed
before first turn may still rotate empty, but must not enter permanent
RequireResume refusal.

---

## Evidence pointers

- Term logs:  
  `~/.local/state/claudia/terms/-Users-marcelo-work-github-com-marcelocantos-jevons/<id>.term`
- Fail-closed messages: `~/.jevons/daily-jevonsd.log`  
  (`auto-start failed` / `refusing to mint a replacement session`)
- Claudia: `registry.go` Materialized; `agent.go` `--session-id` / `--resume`
  and RequireResume gate; `pool.go` JSONL-on-send comment;  
  `agent_test.go` Send/JSONL smoke history
- Jevons recovery (not prevention): `internal/fleet/rehydrate.go` (T313)
- Restarts today: `~/.jevons/restart-daily.log` 14:09–14:53 window

---

## Handoff

**Fix ownership: [claudia](https://github.com/marcelocantos/claudia) repo.**  
This note satisfies T320 acceptance **(1)**. Acceptances **(2)–(4)** wait on
the claudia Materialized (and optionally submit) fix, then daily restart
evidence. Do not implement that change inside jevons alone — Materialized is
owned and persisted by claudia’s registry.
