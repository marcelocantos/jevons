# Post-mortem: invisible token runaway via the detached Claudia fleet (2026-07-06)

Status: incident review · Severity: high (budget) · Author: Claude Code investigation, at owner's request

## Summary

On 2026-07-06 the owner observed token/credit consumption draining
"extremely rapidly" with **no obviously busy session** in any of his
~20+ terminal tabs. The current 5-hour billing block reached 100% and
kept burning after extra credits were unlocked. Investigation found the
drain was **not** any visible tab. It was the **Claudia/Jevons worker
fleet running headless inside a detached `tmux` server** — 47 agent
sessions that survived every closed tab and were invisible to the
operator. A secondary, self-inflicted amplifier (an auto-resuming
multi-agent Workflow) was also found and had already self-terminated.

Root cause is not a bug in a single component; it is a **missing system
property**: Jevons spawns token-spending workers but has **no
real-time cost accounting and no automated clamp-down**. The design for
this already exists (`docs/cost-management.md`) but was filed as "idea,
not scheduled". This incident is the forcing function to schedule it.

## Timeline (local time, 2026-07-06)

- **~17:24** Owner types "continue" in a `~/think` Fable-5 audit session
  (`2438e98d`). It launches a Tier-1 Workflow sweep across 10 repos
  (`wf_ec971479-462`), fanning out ~100+ subagents. Runs unattended.
- **~18:28** That session is interrupted mid-generation, restarts, and
  **auto-resumes the workflow** — re-spawning agents with no human in
  the loop.
- **Investigation window (~18:30–18:45)** Owner asks for help. Estimated
  spend: **~$10.9k over the trailing 2 days**; current 5-hour block
  **~$658 and climbing to 100%**. Cost is dominated by **cache-read
  (~4.5 B tokens / 2 days)** and **cache-creation** — the signature of
  many long-context sessions turning over repeatedly.
- The owner kills the visible spyder tab (a red herring), then closes
  the Workflow's tab. The workflow drains its in-flight agents and
  **freezes at 128 agents** — dead.
- **Burn continues anyway.** The live culprit is isolated: process tree
  under `tmux -S ~/.local/state/claudia/tmux.sock ... -s claudia-anchor`
  (PID 64599, **parented to launchd**), hosting **47 sessions** each
  matching `claude --permission-mode bypassPermissions --disallowedTools
  Agent,TeamCreate,TeamDelete,SendMessage,EnterWorktree`, one carrying
  `--mcp-config ~/.jevons/jevons/.mcp.json`. Several actively cycling
  (one freshly spawned at 41% CPU).
- Fleet killed on owner's explicit instruction (Jevons is experimental,
  no work to preserve): tmux server + 47 sessions + control-mode clients
  killed, socket removed. Top `claude` CPU dropped from 41% → ~8%;
  process count 73 → 34. Burn stopped.

## Why it was invisible (the core failure)

1. **Headless, detached lifecycle.** The fleet lives in a `tmux` server
   parented to `launchd`, not to any terminal. Closing tabs — the
   operator's natural "stop" gesture — does nothing. There is no window
   that shows the fleet, and no single control surface that reaches it.
2. **No token accounting anywhere in the loop.** Jevons spawns workers
   but does not measure their spend. The only visibility is
   `mnemo_usage`, which reads Claude Code's JSONL **after the fact** —
   offline, no live rate, no alerting, no enforcement.
3. **Spend is cache-dominated, so nothing "looks" heavy.** ~96% of cost
   was cache read/creation, not output. No single session appears to be
   generating a lot; the cost is spread across many long-context workers
   each re-reading/re-writing near-full contexts per turn.

## Contributing factors

- **Auto-resume amplifier.** A crashed unattended Workflow re-spawned
  its fan-out with no liveness or budget gate. Automation that
  self-heals into *more* spending is a hazard.
- **Idle-context re-cache tax.** Poking a giant-context session after
  >5 min idle re-pays full cache-creation on the whole context
  (~$11 per message observed on a Jevons session: 5 messages = $57).
  Occasional pokes to bloated-context sessions are silently expensive.
- **Orphan sprawl.** 47 fleet sessions with no owner attached and no
  dead-man's switch. Nothing reconciles "cockpit is gone, so these
  workers should stop."

## What would have caught / stopped this automatically

Mapped to the three layers already sketched in
[`../cost-management.md`](../cost-management.md):

### Layer 1 — real-time collection (prerequisite)
- jevonsd tails each managed worker's JSONL (fsnotify/poll), parses
  `usage` fields, prefers `costUSD`, aggregates into SQLite
  (`usage_events`). Extends 🎯T8.2's "per-worker token counts recorded".

### Layer 2 — live monitoring & anomaly detection (the gap this incident exposed)
- Rolling **cost-rate** per worker, per fleet, and global ($/min, tokens/min).
- **Runaway signals**, any of which trips an alert:
  - global or fleet burn-rate exceeds a configured ceiling;
  - fleet **agent/session count** grows past a bound or spawns unattended;
  - **orphan detection**: fleet sessions whose owning cockpit/attach is gone;
  - projected spend to end-of-block/day exceeds budget.
- Surface a live cost ticker + "what is burning right now" view (the
  question that took an hour of manual `ps`/`lsof`/`mnemo_usage` to answer).

### Layer 3 — automated clamp-down (the property the owner explicitly wants)
- Per-worker / per-fleet / global **budgets** (rate + absolute, per
  hour/day/block).
- **Escalation on breach:** warn → throttle → pause → **kill**,
  configurable, with a **hard ceiling that hard-stops spawning** and a
  single global **kill-switch that actually reaches launchd-detached
  processes** (the manual kill this incident required, automated).
- **Dead-man's switch:** a fleet with no attached owner/heartbeat
  self-terminates rather than running forever.
- **Auto-resume guard:** cap resume attempts; require a liveness +
  budget check before re-spawning a fan-out; never auto-resume
  unattended fan-out work.

## Recommendation

Promote `cost-management.md` from "idea" to scheduled work, sequenced
Layer 1 → 2 → 3, and treat **automated clamp-down** as the headline
acceptance — observability alone would have diagnosed this incident but
not prevented it. Tracked as the target that references this document.

## Verification evidence (this incident)

- Fleet was real and live: 47 `bypassPermissions --disallowedTools`
  sessions under tmux PID 64599 (launchd-parented); ≥1 at 41% CPU.
- Kill was effective: fleet/anchor/clients → 0 processes, socket removed,
  top `claude` CPU 41% → ~8%, `claude` process count 73 → 34.
- Workflow was a separate, already-dead contributor (frozen at 128 agents).
