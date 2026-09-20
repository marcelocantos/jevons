# T679: born-stuck detection scout

Scout only. T679's product acceptance is not implemented or achieved.

## Finding

A bounded birth monitor is needed; adding a label to the current phase
classifier would not cover the reported Grok/Cursor remints safely.

Existing supervision is insufficient:

- `internal/mcpserver/fleet_health.go`: `deadRecoveryPlan` ignores living
  process handles, including a seat whose first prompt never becomes a turn.
- `internal/mcpserver/sentinel.go`: idle-residue supervision requires a decoded
  idle session. Missing transcripts decode as unknown, not idle.
- `internal/mcpserver/fleet_recover.go`: `ClassifyFleetRecover` can classify
  `NeverProgressed && PromptInFlight` as `stuck_busy` (without waiting a real
  birth grace period). Bounce-reminted seats are skipped. This recovery path
  neither proves transcript absence nor supplies the required list status and
  once-only parent notice. It is not T679's oracle.
- `internal/mcpserver/agent_phase.go`: `ReadSessionEvidence` only resolves
  Claude sessions. Other providers return unknown; prior turn/materialization
  evidence can still produce `running`.
- T401 handles addresses after removal, not these still-registered seats.
  T664 protects undecided deliveries from stop/kill; detection must not weaken
  that protection.

## Provider evidence: resolve before implementation

The T416 instrument remains file absence, not transcript growth, payload
grep, receiver behavior, or a missing user-message match. An existing file
containing queue attachments must never count as absent.

The evidence path must actually belong to the current provider and session:

- Claude: `claudia.SessionExists` is the existing authoritative lookup.
- Grok: the conversation file is `updates.jsonl`, not `chat_history.jsonl`.
  `internal/discovery` supports ordinary and exclusive-MCP Grok roots. However,
  `DefaultSessionRoots` omits the latter, and `discovery.TranscriptPath` returns
  an empty string for both lookup failure and absence. Neither alone supports
  an honest absence assertion. Resolve all applicable roots and retain errors.
- Cursor: the pinned Claudia v0.40.0 backend uses `CursorACPStorePath` and
  `store.db` for resume checks (`cursor_acp.go`, `cursor_reap.go`). Its
  `agentStart` supplies no JSONL path override and disables JSONL tailing.
  `agent.go` therefore retains its initially computed Claude-shaped path.
  This is not a Cursor transcript. The pinned backend does not establish a
  first-submit JSONL contract for Cursor.

T501's `liveStreamObserver` in `internal/mcpserver/turn_evidence.go` and T519's
guard in `internal/fleet/migrate.go` explicitly avoid this phantom-path
diagnosis already. Do not bypass them. Merely substituting Cursor's database
existence is also unjustified: the cited resume check is not proof of the
required first-submit creation timing.

Re-slice implementation: first establish an authoritative provider/session
transcript-existence seam and provider-backed fixtures, including Cursor's
first-submit lifecycle. Then implement monitoring against that seam. Until
the seam can distinguish absent from unobservable, report unknown rather
than born-stuck. This prerequisite belongs to T679's cross-provider acceptance;
a Claude-only implementation would not cover its load-bearing example.

## Proposed monitor contract

Use a 120-second grace from accepted opening/remint prompt, not registration
or process launch. This is a proposed policy, confidence 0.65: it gives more
headroom than the existing 45-second turn-confirm window while bounding
parental uncertainty to minutes. It is not a measured latency percentile.
Cold session loading precedes acceptance and must not consume the grace.

Key state by name, provider, and current session ID. Repeated nudges must not
restart the clock. Record acceptance on all relevant paths, including
`submitCursorStartBrief`, ordinary start/send, and remint handover. A fresh
seat with no accepted prompt must not be accused. A remint gets new state;
stale turn evidence for the old session must not suppress the finding.

After the deadline, authoritative file absence produces `born-stuck` in
`jevons_agent_list` and one notice to the registry parent, naming provider,
session, elapsed time, and that no transcript ever appeared. Run the same
monitor from a periodic health hook so no list call is required. Keep PINNED
information visible alongside the birth diagnosis.

Use deliver-by-name for the notice. A queued/accepted notice counts as
submitted; an unsubmitted notice needs retry without manufacturing success.
Persist the notification identity if once-only must survive daemon restarts.
Exactly-once receipt across a crash needs an idempotent delivery key, not just
an in-memory boolean. Do not automatically resend the opening prompt, stop,
kill, or migrate as a side effect of diagnosis.

## Required oracle

Drive a fake clock and a real temporary transcript path through registered
seat -> accepted prompt in flight -> missing transcript:

1. Before 120 seconds: no born-stuck claim or notice.
2. At the deadline: list row marks born-stuck; parent receives one notice
   naming provider and transcript absence.
3. Repeated list calls and periodic sweeps: no duplicate notice.
4. Transcript appears: diagnosis clears, including a queue-attachment-only
   file without a matching authored user message.
5. Remint under the same name: old evidence/notice cannot decide the new birth.
6. Permission failure, unresolved provider path, and phantom Claude path for
   Cursor: unknown, never positive absence.
7. No accepted prompt: no finding. Repeated nudges: original deadline remains.
8. Notice failure and daemon restart: retry/dedup follow the chosen durable
   notification contract.

Provider lookup fixtures must exercise actual path resolution rather than
only injected booleans. A hermetic exception is reasonable for the clock and
dedup policy, but cannot establish a provider's first-submit storage contract.
Product completion still needs development-surface observation after activation.

## Scout validation and delivery

No product source was changed. The requested full Go gate was run and its
specific persisted record reread; no shared-host `gate last` was used:

```text
GATE make-test-go exit=2 RED id=c9f180bf out=0e0ecb4ae10c dur=2m36.3s tree=dirty+11@c67728aded32
```

The run reported 16 failures, 1,800 passed, and 3 skipped. It emitted a Go
module-cache permission denial and included worktree/doc-ratchet failures;
it does not isolate the two failures attributed to T680 in the brief. No
green claim or T680 repair is made.

The attempted scout delivery to `jevons-po` was rejected by the tool layer:
`MCP tool call requires approval, but approval policy is never`. The report
was not submitted. This document preserves the handoff for the parent.
