Diagnosis complete, all from HEAD (those three files are clean vs HEAD — not another worker's WIP). Reporting before changing anything, as instructed.

## 🎯T387 — determination

**Answer to clause 2's distinction: the second horn. 🎯T305 *does* cover this path. `prompt_delivered` asserts something strictly weaker than a begun turn.**

### Evidence — four dead spawns, read first-hand from the log and session files

| agent | spawn | `prompt_delivered=true` | turn began? | JSONL born |
|---|---|---|---|---|
| jv-t387 (me) | 21:48:11 `7d4b260a` | 21:48:12, again 21:53:48 | no | 21:57:05, on the *repressure* |
| jv-t375 | 21:52:01 `3425f688` | 21:52:02, again 21:53:37 | no | 21:57:05, on the repressure |
| cl-t20-env-leak | 21:39:32 `59303995` | 21:39:33, again 21:46:14 | no | 21:47:21 |
| cl-t2.8-broker-oracles | 21:40:40 `a1aca79c` | 21:40:43, again 21:46:35 | no | 21:47:21 |

Eight `prompt_delivered=true` lines across four agents; **zero** turns began from any of them. Every turn that actually ran came from a later nudge. Each original session then hit `auto-start failed … JSONL not found … refusing to mint a replacement session`, and my own row shows `sentinel.repair symptoms=[idle:jv-t387 …]` at 21:56:40 — the sentinel reading it as ordinary idle, exactly as the target predicted.

One correction to your summary: the claudia pair's JSONL was never created **for their original session ids** — true. Their current files exist because they ran after rehydrate. The dead-spawn shape is identical across all four, including both jevons ones.

### Why T305 misses it, from source

`agents.go:319` does call `deliverStartPrompt`, and failure does stop the process and error the tool. Coverage is present. The predicate is what fails:

`ConfirmSendBeganTurn(status, err)` (`delivery_confirm.go:46`) takes only a **status string** → `deliverToSender` sets `status="sent"` iff `proc.Send()` returned nil (`agent_send.go:184`) → claudia `Agent.Send` → `tmuxagent.SendKeys` → `tmux send-keys -l msg`, `tmux send-keys -l "\r"`, nil iff both **tmux processes exited 0**.

So `prompt_delivered=true` means *two tmux commands exited 0*. Nothing in the chain observes the composer, the pane, the JSONL, or any turn event. `ConfirmSendBeganTurn` is a status-string validator that cannot distinguish "turn began" from "keystrokes went into a pane that swallowed them" — its name overstates its postcondition.

Aggravating, and the reason T305's own detector stayed silent: `deliverToSender` also calls `markAgentTurnBegan` on that same weak signal (`agent_send.go:195`), so `ClassifyAgentListStatus` flips `never_briefed`→`running`. The mechanism built to catch an unbegun turn is set by the signal that fails to detect one.

### Second, independent finding — the running binary lacks the fixes

`go.mod` pins claudia `06025ef` (2026-08-03). None of these are in it (`merge-base --is-ancestor`, exit=1 each):

- `0671f0e` **"submit large pastes; confirm turn began (T305)"** (08-08 19:11)
- `52bab3f` "wait out /rc connecting before paste submit (T305)" (08-08 19:16)
- `c7ba363` "Materialized only after conversation evidence" (T320) (08-08 15:09)

There is **no `replace` directive**. jevons `9f76dcc` added `replace … => ~/work/…/claudia` on 08-08 15:13 as T320's wiring; it is gone. So both T305's and T320's attestations describe behaviour the daily daemon does not have — which independently explains `materialized: true` on all four rows and the "refusing to mint a replacement session" errors.

The HEAD comment at `agent_send.go:192` says it outright: *"confirmed Send (incl. paste-block press-through in claudia) means a turn began"* — it assumes a claudia press-through that is not in the build.

### Fix boundary — plainly

`0671f0e` already does the real work: bracketed-paste for multi-KB briefs, named-Enter press-through the `[Pasted text #N]` chip, and **errors if the composer never leaves idle** — genuine pane observation, with hermetics for stuck paste and empty pane. **That mechanism belongs to claudia and largely exists; cl-t21-turn-begin-confirm is hardening the same surface. I will not duplicate pane observation in jevons.**

What is genuinely jevons-side, and what I'd do next on your go-ahead:

1. **Repin claudia** past `0671f0e`/`52bab3f`/`c7ba363` — the T320 `replace` regression. Nothing else matters until this lands.
2. **Stop manufacturing turn evidence from a status string** — `markAgentTurnBegan` must key off conversation evidence, not `Send()==nil`, so `never_briefed` can actually fire. That is the jevons half of clause 2.

Holding here per your instruction. Say go and I'll take (1) and (2) with mutation evidence in a separate worktree — including the over-broadness mutation — or say so if you'd rather cl-t21 land first and I rebase onto it.