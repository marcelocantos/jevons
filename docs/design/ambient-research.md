# Ambient research (🎯T356)

Status: built (staff-cycle shape). Owner-facing residual noted at the end.

## Problem

Research that only happens when the owner asks for it dies in scrollback. What
the fleet learned last week is not written down anywhere durable, prior
conclusions get quietly replaced by newer ones, and nothing outside the owner's
turn can start a research pass.

## Shape

A standing **staff cycle** (🎯T325 shape), not a babysat research session and
not a permanent monologue. It lives in `internal/research` and runs inside
`jevonsd`:

```
periodic tick ─┐
               ├─ scan surrounding context ─┐
feed poll  ────┘                            ├─ fold into durable notes ─→ brief overseer
                                            │        (versioned)
                          new feed items ───┘
```

Two triggers, one note store:

- **Periodic context refresh** (default 90 min, 72 h lookback): this repo's git
  history by scope, sibling repositories under the same org root, the
  convergence frontier (`bullseye.yaml`), the eventlog tail, and recent agent
  sessions.
- **Async feed trigger** (default 30 min, off until subscribed): each
  subscribed feed is fetched, parsed (RSS 2.0 or Atom), and new items kick a
  bounded cycle without waiting for an owner turn.

## Durable, versioned notes

`state_dir/research/notes.json` is canonical; each note is also rendered to
`state_dir/research/notes/<id>.md` for humans. One note per topic
(`repo/jevons`, `context/frontier`, `context/fleet`, `feed/<name>`), and within
a note one **finding** per stable subject key.

The rail is **no silent overwrite**:

| Observation | Outcome |
|---|---|
| unseen key | finding added, status `current` |
| same key, same claim | confirmed — provenance refreshed, **no revision** |
| same key, new claim | prior explicitly superseded (retained, with `superseded_by`), new claim added |

A revision is appended only when something was added or superseded, so a quiet
context produces a quiet note and no brief.

### Claims are written to survive a clock tick

A claim built from a raw count inside a sliding window would supersede itself on
every cycle. Volumes are therefore quantized into activity bands
(quiet/light/steady/heavy) and the exact numbers ride along as *evidence*, which
never triggers a supersession. A scope's claim changes when new work lands there
— which is the signal worth recording — not when the window slid five minutes.

## Bounds

Every ambient loop needs a reason it cannot run away:

- `max_briefs_per_hour` (2) — delivery budget to the overseer.
- `max_feed_cycles_per_hour` (4), `max_feed_items` (10) — feed trigger budget.
- `max_commits` (300), `max_repos` (6), `lookback_hours` (72) — scan bounds.
- `allowed_hosts` — **opt-in**: an empty allowlist allows nothing. Subscribing a
  feed allows its host; the agent fetches exactly the subscribed URLs and never
  follows links out of them. This is a subscription, not a web crawl.
- Feed responses are size- and time-bounded (2 MB, 20 s).
- Feed cursors remember seen item ids, so an unmoved feed kicks nothing.

## Surfaces

| Surface | Use |
|---|---|
| `jevons_research_cycle` | run a cycle now (`context` \| `feed` \| `both`) |
| `jevons_research_list` / `jevons_research_read` | listable notes + markdown |
| `jevons_research_configure` | cadence, bounds, feed subscriptions |
| `jevons_research_status` | run record + per-feed cursors |
| `GET /api/research/notes`, `/api/research/notes/{id}` | product API |
| `state_dir/research/notes/<id>.md` | file path for anything else |

Briefs go to the overseer through the same fire-and-forget path the RSI coach
uses. Like the coach, the research cycle **never files bullseye** — it reports;
the overseer decides.

Daemon env: `JEVONS_RESEARCH=0` disables the schedule (tools stay registered);
`JEVONS_RESEARCH_INTERVAL` overrides the cadence.

## Oracles

Hermetic, in `internal/research`, `internal/mcpserver`, and `internal/server`:

- periodic tick runs a cycle unattended and leaves a durable note
  (`TestScheduledTickRunsCycleUnattended`)
- feed-triggered cycle writes a note with feed provenance, and an unmoved feed
  kicks nothing (`TestFeedTriggeredCycleWritesNoteAndIsBounded`)
- supersession keeps both conclusions with provenance
  (`TestApplySupersedesPriorConclusionExplicitly`)
- re-observation writes no revision (`TestApplyUnchangedObservationWritesNoRevision`)
- a host outside the policy is never fetched
  (`TestPollFeedsRefusesHostsOutsideThePolicy`)
- notes are listable and readable over the API (`TestResearchNotesListAndRead`)

## Residual

- **Class-3 (owner taste):** which feeds are worth subscribing to, and how
  findings should be ranked. The vendor choice is deliberately not baked in.
- Findings are **mechanical digests**, not model-authored analysis. Spawning a
  fleet worker for a deep dive on a moved note is a natural next slice; the note
  store and the trigger are the part that had to exist first.
- No web crawl beyond subscribed feed URLs, by policy.
