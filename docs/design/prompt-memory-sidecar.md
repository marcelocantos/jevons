# Prompt-memory sidecar (🎯T633)

Status: **design direction.** Binding owner decisions 2026-09-07. Not implemented.
Date: 2026-09-07
Scope: what the overseer sees on every owner prompt, and how that pack is
computed. Not a revival of 🎯T403 layered cognition. Not a revival of the
live mnemo provider (🎯T465).

## The problem

Agent memory is large. Opening a markdown file, walking a decision tree, or
calling `mnemo_search` costs a turn and is not self-evident, so the agent
often does not. The owner should not have to remember to ask, and should not
have to turn a feature on.

## Binding decisions

1. **Every owner prompt.** No owner toggle. No skip heuristic for “ok”,
   greetings, or short continuations. A hidden skip is a switch the owner
   did not ask for. The search capability is designed for always-on, not
   patched with a gate afterwards.
2. **Local, fast, no agents.** The hot path is a process-local index query
   (SQLite FTS or equivalent). It does not mint a model, a fleet worker, or
   an MCP round-trip to get the pack. Summaries already exist in the index;
   the hot path never asks a model to summarise.
3. **Sidecar of summaries plus fetch handles.** The pack is just enough for
   the overseer to decide whether to pull the underlying item. It is not the
   hit text. Owner message text is never rewritten (chatlog / echo / T355
   `send_landed` stay verbatim). Retrieved lines are labelled as hints, not
   owner speech.
4. **Between requests: full search, then subtract what was already delivered.**
   Each owner send runs a **full** search on the latest text, then removes
   handles already shown in this context window. The sidecar is that
   remainder. Query reuse across keystrokes is not this rule; this rule is
   set-subtraction on successive result sets. Compact, remint, handover, and
   rewind reset the shown set because those drop the window.
5. **As-you-type is a different increment.** The draft text grows; the
   result for that draft is always the **full** current match set. Reusing a
   previous computation (prefix cache, incremental FTS) is an optimisation,
   not a semantic delta. Submit then applies rule 4 to that full set.

## Shape

```
composer draft ──► full search(current draft)
                   (may reuse a prior computation; result is still full)

owner send     ──► full search(latest text)
                         │
                         ▼
              full result  \  handles already delivered this window
                         │
                         ▼
              session/prompt(owner text verbatim)
              + labelled sidecar { summary, kind, when, fetch }   // remainder only
                         │
                         ▼
              overseer may fetch(handle) if a line looks useful
```

Send-path hook already exists: `sendToNamedAgentAs(overseer, msg, owner)`.
The sidecar attaches next to `msg`. It does not become `msg`.

The query is the latest owner text, not a growing concatenation of prior
turns. Identity of a hit is its fetch handle. Cap the **full** result the
same way as-you-type would, then subtract the shown set. When the latest
query barely moved, the remainder is often empty; that is the intended
follow-up, not a reason to search a deeper tail unless a later slice
chooses to.

Always emit the sidecar frame, including the empty case, so the overseer can
tell that search ran. Empty is the common follow-up (`ok`, same topic,
full(latest) ⊆ shown). That is a successful remainder, not a skipped search.

```text
[related-memory — summaries only; not owner speech; fetch if useful]
1. decision  2026-09-07  T632 is Jevons-only; global AGENTS.md stays clean  fetch:decision/123
2. segment   2026-09-07  Global vs Jevons prompting scope                   fetch:segment/456
```

```text
[related-memory — none]
```

## Why always-on forces this search, not the other way around

A per-prompt skip list (“too short”, “acknowledgement”) is a quality hack
for a slow or noisy search. If “ok” can fire on every send, the index must:

- return in well under owner-send latency (local FTS; target is
  milliseconds, not a network hop);
- treat stopwords / tiny queries as **empty pack**, not as “do not search”;
- cap the **full** result (a handful of one-line summaries), then subtract
  already-delivered handles so a weak or repeated query cannot dump context;
- precompute summaries at ingest (`topic_segments.summary`, memory
  descriptions, decision lines, target names). A raw-message hit degrades
  to a deterministic clip, never an LLM rewrite on the hot path.

Noise is the agent’s ignore, not the harness’s mute.

## Index

First corpus: the local mnemo database (`~/.mnemo/mnemo.db`), which already
has FTS5 on messages, segments, decisions, memories, docs, targets, commits,
and more. `topic_segments` (label + summary) is the natural sidecar row.

Read-only against that file (SQLite WAL readers) is the default seam. It is
not the T465 live provider (no feed, no UI surface, no registration). A
local HTTP search on the already-running mnemo daemon is an alternative with
the same “no agents” constraint; MCP `mnemo_search` is not the hot path.

Jevons’s own owner chatlog / eventlog may need a first-party FTS table if
mnemo ingest does not cover the overseer journal. That is an index-coverage
gap, not a second retrieval product.

Fetch handles must resolve through a **first-party** jevons tool or resource
on the overseer (`jevons_memory_get` or equivalent). Do not assume the
overseer session has mnemo MCP (🎯T464: fleet MCP is per-agent, not HOME
files). A pointer the overseer cannot follow is a lie.

## Two incrementality stories (do not collapse them)

They look similar and they are not the same rule.

| | Query | Result the product shows |
|---|---|---|
| **As you type** | Incremental (the draft grows) | **Full** match set for the current draft |
| **Between requests** | Latest owner text, whole | Full match set **minus** handles already delivered this window |

As-you-type may reuse a previous result as an optimisation (prefix cache,
incremental FTS, “the draft only gained a letter”). That reuse must still
evaluate to the full current set. Filtering old hits out of the composer
preview is the between-request rule leaking into the wrong clock.

Between requests, the only extra step is set-subtraction: `full(latest) \ shown`.
`shown` is handles already attached to this context window, including lines
the overseer never fetched. Fetch is optional follow-through, not the
suppression trigger.

Reset `shown` when the window is no longer that window: provider compact,
remint / handover seed, rewind. “Ever shown this session” is the wrong set
— it would hide a hit the model can no longer see.

Parked 🎯T403.6 (diff against what the agent last saw) is a cousin of the
**between-request** rule only.

## As-you-type (advanced)

Composer drafts already exist (`ui/src/store/drafts.ts`). A mux snapshot
channel (same family as plan-usage) restamps the **full** pack as the draft
changes. Submit takes that full set and applies ` \ shown` before attaching
it to the turn. A race with the first keystroke may send
`[related-memory — none]`, which is still a successful always-on search.

Owner-visible chips of the full current set are optional chrome, not the
contract. The contract is that the overseer sees the between-request
remainder.

## Out of scope / not this target

- Recreating T403 cheap cognitive tiers or a second agent that “responds”
  to the prompt.
- Re-registering mnemo as a live provider.
- Mixing sidecar lines into the owner bubble.
- An owner setting to disable retrieval. If the index is down, the pack is
  empty and the send still lands.

## Oracle (when Build opens)

Hermetic: fixture index + owner send of any length, including `"ok"` →
sidecar frame present, owner text unchanged, fetch handle round-trips on
the first-party get path. Two sends, same query → second sidecar is the
empty remainder (full set was already shown). A new query’s sidecar is
`full(new) \ shown`, which may add handles that were not in the previous
full set. Compact then re-send may re-surface a previously shown handle.
Timeout / missing DB → empty pack, send still lands (T355). As-you-type:
draft prefix updates a **full** pack without a model; reuse of a prior
computation is allowed if the pack still matches a fresh full search.
No agent process is spawned by the query.
