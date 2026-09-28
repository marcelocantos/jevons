# Oh My Pi as Claudia's backend

Recovered 2026-09-24 from a Cursor cloud chat whose decisions were
made 2026-09-23. The prose below is that chat, pasted because the app
could not save it. One broken arrow in the launch path is restored.
The Claudia pin named in the original ("v0.34.0") is the pin as of
that day; `go.mod` in this checkout is `v0.42.0`.

Decisions below are the ones already made (2026-09-23). Gaps are
marked as open questions. Claudia's own source is not in the jevons
checkout; the seam is what this repo calls
(`github.com/marcelocantos/claudia` v0.34.0 in go.mod,
`internal/fleet`, `docs/architecture-current.md`). Package facts are
from the oh-my-pi tree fetched the same day.

## Problem

Jevons does not talk to model APIs. jevonsd spawns and supervises a
vendor harness process through Claudia, and every vendor leaked its
own files, login home, transcript dialect, and usage scraper into the
daemon.

The path today:

```
butler.Fleet  →  internal/fleet.Claudia  →  claudia.Registry (~/.jevons/agents.json)
                                              →  Agent.Launch / Send / Migrate
                                                 →  a vendor CLI
```

Launch calls `reg.Launch`, then WaitReady (45s). Send requires that
live process. Provider ids this repo switches on are grok, claude,
codex, cursor, bedrock, plus the string `xai` as a Grok alias.
Selection is a string (`JEVONS_PROVIDER`, then config, then grok), not
an allow-list, so a new backend is not blocked at the jevons edge.

What that costs, per vendor:

| | Grok | Claude | Codex | Cursor |
|---|---|---|---|---|
| Process | ACP JSON-RPC (`session/new`, `session/load`, `session/prompt`). Connect-mode is a detached `grok agent serve` plus WebSocket (`CLAUDIA_GROK_CONNECT`). | A tmux pane Claudia owns. Ready means the composer accepts input (`tmuxagent.MatchReady`); the splash used to satisfy the pattern and drop the submit key. | App-server. `thread/start` has no MCP field, so exclusive launch writes `CODEX_HOME`. | ACP seat. Remint problems; leftover stdio clients get reaped unless the broker holds them. |
| Transcript | `~/.grok/sessions/…/updates.jsonl` (`chat_history.jsonl` is never the source) | `~/.claude/projects/…/<id>.jsonl` | rollout JSONL under `~/.codex` | dashboard JSON or `ai-code-tracking.db` — not a bill |
| MCP | ACP `session/new` gets only this daemon's HTTP MCP. A Claude-shaped payload was Invalid params. | Discovered MCP plus jevonsmcp | `CODEX_HOME` | Discovered MCP plus jevonsmcp |

Homes scrubbed on the way: `~/.claude.json`, `~/.grok`, `~/.codex`,
`~/.cursor/mcp.json`.

Around that, jevons grew a reader per dialect: `internal/discovery`
(Grok and Claude only), `internal/transcript` (Claude compact /
parentUuid, Codex session_meta / response_item / rollback, Grok
updates.jsonl), `internal/turnev` (the only JSONL decoder, built
around Claude queued-command attachments), `internal/cost` (Grok
costUsdTicks, a Claude message.usage subset), `internal/harnessusage`
(four scrapers), `internal/turndepth` (a Claude Code hook). Plan
windows are Claudia's QueryPlanUsage, reshaped in
`internal/planusage`. A Claude session id can be registered with no
JSONL, because no turn was submitted, and fail-closed resume then
looks like the file vanished.

The carousel that produced this: plans run dry, the next CLI comes
back, and the contract that does not match (token accounting, a
missing MCP field, ACP remint, a hook as the only outside view of a
turn) lands in the daemon. The durability rule under all of it is
that no conversation is lost.

## Why these two packages

Canonical repo: https://github.com/can1357/oh-my-pi. CLI name `omp`.
MIT. TypeScript on Bun (`engines.bun` >= 1.3.14). It is a fork of
Mario Zechner's Pi (https://github.com/badlogic/pi-mono).

Three things share the word "backend". Only the first two are in this
design. The third is a search-provider chain and is irrelevant here.

| Package | Role |
|---|---|
| `@oh-my-pi/pi-ai` | The LLM client. Model catalog, OAuth and API keys, stream / complete, tool-call events, usage windows. On main at fetch time, package version 18.2.11. "main" is `./src/index.ts` — TypeScript source, not a Go module and not a binary. Tool execution is not in this library: it returns the call, the caller appends a toolResult. |
| `@oh-my-pi/pi-agent-core` | The turn loop on top of pi-ai (`packages/agent`, import `@oh-my-pi/pi-agent`). Also depends on snapcompact, pi-wire, pi-natives, and OpenTelemetry. prompt / continue / agentLoop stream a model call, run registered tools, append results, and start the next turn. steer and followUp inject mid-tool or after the model would have stopped. transformContext runs before each call. Compaction is a separate export backed by snapcompact. abort, queue mode, and getApiKey cover cancel, queued input, and expiring tokens. |
| `omp` (`@oh-my-pi/pi-coding-agent`) | The coding agent: its own bash/read/edit, sessions, subagents, TUI, `omp -p`, `omp --mode rpc`, `omp acp`. That is a second agent. It is not the backend. |

pi-ai already speaks the wire dialects the plans use
(openai-completions, openai-responses, openai-codex-responses,
anthropic-messages, bedrock-converse-stream, google-generative-ai,
google-gemini-cli, google-vertex, and the Cursor and xAI modules).
`stream()` yields `text_*`, `thinking_*`, `toolcall_*`, `done`,
`error`. The caller owns the Context and can serialize it.
Cross-provider handoff rewrites another provider's thinking blocks
into `<thinking>` text and keeps tool calls.

Plans already in use, and the Oh My Pi provider that matches:

| Plan | Provider id | Auth |
|---|---|---|
| Claude Pro/Max | `anthropic` | OAuth. Env `ANTHROPIC_OAUTH_TOKEN`, then `ANTHROPIC_API_KEY`. |
| ChatGPT Plus/Pro (Codex) | `openai-codex` | OAuth. An `OPENAI_API_KEY` enables `openai/*` only, not Codex subscription models. |
| Cursor | `cursor` | OAuth (PKCE). Env `CURSOR_ACCESS_TOKEN`. |
| SuperGrok | `xai-oauth` | Device code against `https://auth.x.ai`. Env `XAI_OAUTH_TOKEN`. Distinct from `xai` + `XAI_API_KEY`. |
| xAI API | `xai` | `XAI_API_KEY` |
| Amazon Bedrock | `amazon-bedrock` | AWS creds |

Credentials resolve in a fixed order: runtime key, models.yml, stored
OAuth with refresh and multi-account rotation, login-stored API key,
env, other stored key, models.yml fallback. Local store is SQLite
`~/.omp/agent/agent.db`. `OMP_AUTH_BROKER_URL` is a credential broker
(redacted refresh tokens, `POST /v1/credential/:id/refresh`). It is
not Claudia's seat broker.

A Rust addon, `@oh-my-pi/pi-natives`, does search, shell, AST, and
PTY. It stays out. So do omp's own tools.

pi-agent-core is the piece that makes pi-ai enough. pi-ai alone is
one HTTP stream; the fleet needs a turn loop that will call `jevons_*`
and come back. That loop already exists. Register `jevons_*` as
AgentTools whose execute calls back into Go. Leave the coding agent's
tools out.

## Decision

The first slice keeps Claudia. Jevons and YTT keep their current
Claudia pin. Claudia's vendor backends are replaced by one thin Bun
sidecar over `@oh-my-pi/pi-agent-core` (providers via
`@oh-my-pi/pi-ai`). YTT is not this slice.

Later, after the fleet is actually running on that backend, the
remaining Go moves into the jevons repo: the registry, the seat
broker, the MCP tool bodies, and the conversation log. The seat
broker stays a separate process, so a jevonsd bounce still reclaims
seats by name.

Not a Go port of the OAuth matrix. That recreates the carousel inside
Claudia. Not `omp --mode rpc` or `omp acp` as the first cut. Those
are already a door for a non-Node host, and they are a second agent.
Use them only if the goal changes from replacing the provider layer
to replacing Claudia.

## Where the shim sits

Claudia stays the seat owner both products already call. The registry
file stays `~/.jevons/agents.json` (`claudia.NewRegistry` in
`cmd/jevonsd`). `internal/fleet.Claudia` stays the `butler.Fleet`
implementation: policy in the butler, mechanism in the registry.

The shim replaces the vendor process behind that registry, at the
slot Launch / Send / Migrate already occupy:

```
jevonsd
  fleet.Claudia.Launch / Send / Migrate
    claudia.Registry                         unchanged call
      one new backend                        replaces grok | claude | codex | cursor
        Bun sidecar                          one process, one pi-agent-core Agent per seat
          pi-ai                              the provider HTTP call
```

jevonsd still does not import TypeScript. The sidecar is a process
Claudia's new backend dials. It does not exist upstream: pi-ai and
pi-agent-core are libraries, and the RPC and ACP servers belong to
omp, which this slice does not use.

When the Claudia daemon is up, `claudia.BrokerAvailable` is true
(`CLAUDIA_BROKER_SOCKET`; `CLAUDIA_NO_BROKER` forces the old mode).
The daemon parents every seat. jevonsd shutdown does not StopAll.
Boot reclaims by name (`StartAllPreferAdopt`) and does not nudge
surviving workers. The sidecar's lifetime matches that: jevonsd
restarts leave it up. A backend upgrade is a deliberate restart of
the sidecar, not a side effect of bouncing the daemon.

## The sidecar

Extremely thin. One long-lived Bun process. One Agent per seat. It
does not reimplement the loop.

IPC, and nothing else:

- prompt
- steer
- abort
- the event stream back to Go
- a tool callback into Go for `jevons_*`

Pin a version. 18.0.0 landed 2026-08-22 and 18.2.11 landed
2026-09-23, several releases a week. The recent fixes are the
long-seat failures: stream hang, compaction dropping history,
long-turn CPU, steering that skips tools. Floating on main
reintroduces those. Which release is pinned is an open question;
that it is pinned is not.

Persist the agent context on each `turn_end`. That snapshot is how
the chosen restart reloads the seat. It is not the mnemo log.

## What stays in Jevons

Live broker switch. An Agent lives in one process. `setModel` changes
the provider inside that process. Nothing in omp moves a live seat,
its open stream, or its in-flight tool calls onto another broker. An
upgrade handoff is quiesce, snapshot, stop, and let the new pin
`continue()` from the snapshot. That handoff stays here.

Migrate seed. Claudia v0.34.0 `Agent.Migrate` splits.

What survives is the continuity contract, still enforced on the way
through `internal/fleet.remapViaClaudia` (`live.Migrate`,
`MigrateArgs`):

- same seat handle
- a same-provider switch is SetModel, not a migrate
- refuse while a turn is in flight
- never `session/load` the predecessor
- distill at most 64 live turns into a 4000-rune inert seed: last user, last assistant, tool names marked do-not-invoke, goal, file hints
- refuse a cold seed unless Force
- publish `model_switch` and send the seed once
- after the broker reports the remap, do not deliver a second seed (🎯T622 / 🎯T646.1)

GatherBrief and Distill stay host-side. The inert seed exists because
predecessor tools are foreign. With one `jevons_*` tool set on every
plan, keeping the pi-agent-core context and calling `setModel` is the
default. The seed stays the cold-start path.

What does not survive is `swapBackend`: StartAgent on a vendor CLI,
stop the old process, tmux, a JSONL tail, connect URL and PID,
ready-detection, and the per-provider capability matrix (sandbox,
extra args, permission mode, terminal log).

For this slice the registry, the broker socket, reclaim-by-name, and
that continuity contract stay in Claudia. Jevons and YTT keep the pin
they have.

## Session log

pi-ai and pi-agent-core do not write a session file. Resume, inspect,
distill, and cost today read vendor JSONL. The fail-closed resume
contract moves to a log the shim writes and Jevons owns.

The spool is one global dated log. Not a per-agent file. Not a rename
rotation. Not a change to pi-agent-core to make it log. Not omp's
SessionManager.

The shim appends newline-delimited records. Each record names its
seat. The file is `events-YYYY-MM-DD.log`, UTC, chosen by the event's
own timestamp. The shim finishes the line before it opens the next
day. A byte cursor still covers the live day.

When a newer date appears, every older date is closed: the shim has
stopped appending to it. mnemo then:

- drains those files in order
- commits
- checkpoints
- compresses each closed file in place with APFS/HFS transparent compression (decmpfs, via `ditto --hfsCompression` or applesauce)

A normal read still returns the log bytes. A rewrite expands the
file, so compression happens only after the shim has stopped
appending to that date. The compressed file stays the rebuild source
if mnemo's own database is what went bad.

mnemo watches every file in the directory, not only the newest name,
and drains older dates in order before it treats the live day as the
only tail.

mnemo down or behind. The shim keeps appending. Closed days sit
uncompressed until mnemo has drained, committed, and checkpointed
them. A few days asleep is a backlog, not a skip. mnemo does not jump
to the live tail and abandon the older files. The `turn_end` snapshot
inside the sidecar is untouched by any of this; that one reloads a
seat.

The directory path, and what happens to an event whose timestamp
falls on a day already closed, are open questions.

## What mnemo watches today

mnemo watches three trees, written by the CLI harnesses, not by
Claudia:

| Tree | Who writes it |
|---|---|
| `~/.claude/projects/` | Claude Code |
| `~/.codex/sessions/` | Codex |
| `~/.grok/sessions/` | Grok Build |

Jevons has its own readers on the same habit. `internal/discovery`
scans Grok `updates.jsonl` and Claude `<id>.jsonl`, and has no Codex
or Cursor root. `internal/harnessusage` adds Codex `rollout-*.jsonl`
under `~/.codex` and a Cursor dashboard export or
`ai-code-tracking.db`. `internal/turnev` decodes Claude. None of them
are mnemo.

An OMP backend that skips the process costume stops appending those
three trees. New seats drop out of mnemo until mnemo watches the
dated-log directory. History already on disk stays indexed.

omp's own journal is the wrong substitute. The coding agent
(`SessionManager` in pi-coding-agent) writes an append-only file at
`~/.omp/agent/sessions/<encoded-cwd>/<timestamp>_<sessionId>.jsonl`,
with large payloads in `~/.omp/agent/blobs/<sha256>`. Header is
`type: "session"`, version 3. Later lines are a tree (`id` /
`parentId`) of message, compaction, model_change, and similar
entries. `PI_CODING_AGENT_DIR` and `--session-dir` move the root.
`--no-session` writes nothing. The shim gets that file only by using
SessionManager, which pulls the coding agent in. pi-ai and
pi-agent-core do not write it.

If the only new bytes were that journal:

- mnemo would not see them — it is still watching the three vendor trees
- the layout is one file per session, split across a blob store, which is the shape this design refuses
- a byte cursor on "the newest file" misses every other seat and every closed day
- existing vendor history would stay indexed and then stop growing, with no single directory that contains both

## First slice

Ships:

- Claudia stays the seat owner: registry, broker socket, reclaim-by-name, `Agent.Migrate` continuity contract.
- One new Claudia backend. Vendor CLIs are no longer what Launch starts.
- A pinned Bun sidecar: one process, one pi-agent-core Agent per seat, pi-ai for the provider call, the five IPC verbs above.
- `jevons_*` registered as tools whose execute calls back into Go. pi-natives and omp's tools stay out.
- Context saved on each `turn_end`, so a deliberate sidecar restart reloads the seat.
- The global dated log, written by the shim. mnemo watches every file in that directory, drains older dates in order, and compresses a day only after it is closed and checkpointed.
- Same-provider switch is `setModel`. The inert seed remains the cold-start path. Jevons still owns the handover brief and still does not deliver a second seed after a remap.

Waits:

- Moving the registry, the seat-broker process, the MCP tool bodies, and the conversation log into the jevons repo. Trigger is the fleet running on the sidecar, not the first green seat.
- YTT. It keeps its Claudia pin.
- Retiring Claudia.
- Pointing every jevons reader (discovery, transcript, turnev, cost, harnessusage, turndepth, the chat-wire normalizer) at the new log as one folded owner. Resume cannot keep depending on a vendor file the sidecar will not write; how much of that reader work is in the first slice is open.
- omp as the agent.

## Risks

Grok and Claude are not the same product on the far side of OAuth.
Fleet Grok is the Grok Build CLI: `~/.grok/sessions`,
`updates.jsonl`, model ids like `grok-4.6`. SuperGrok is
`xai-oauth`, xAI's HTTP API, gated on an X account. An API key is a
third provider id, `xai`. Swapping the CLI for SuperGrok changes
models, billing, and the session store. It is not a drop-in for
ProviderGrok.

Claude Pro/Max may bill this client as extra usage. Upstream Pi's
docs say third-party harness use of Claude Pro/Max draws extra usage,
billed per token, outside included plan limits. Oh My Pi does not
repeat that sentence. It does surface a display-only
`anthropic:extra` USD row, and it does not use that row for
credential ranking. Included-plan quota is Anthropic's policy. Login
succeeding does not mean the plan limits are what is being spent. Oh
My Pi will show the USD; it will not move that spend back onto
included limits.

Cursor OAuth is not the Cursor IDE agent. Today's Cursor seat is a
CLI with ACP remint issues. `cursor` in omp is an HTTP model API on a
Cursor account. Editor-agent behavior does not come along.

Existing logins do not move. Tokens in `~/.claude`, `~/.codex`,
`~/.grok`, and `~/.cursor` are not `agent.db`. Each provider still
needs an omp login.

The control plane shrinks because the reason for it goes away. Seat
parenting, exclusive MCP homes, splash detection, and per-CLI ready
signals exist to supervise vendor TUIs. A stream client deletes the
reason for most of them. WaitReady today means "the pane accepts a
keystroke". On the sidecar it has to mean "this seat's Agent is
loaded", or Launch still blocks for 45 seconds on a signal that will
never come. That is a product change in the new backend, not a
library swap.

Tool bodies stay in Go. The loop runs in Bun. `jevons_*`
implementations stay here. A callback that fails or stalls stalls the
turn; the sidecar must surface that as the tool status pi-agent-core
already has (ok, error, blocked, timeout, aborted), not as a hung
Agent.

Pin drift. Several releases a week, and the fixes are exactly the
bugs a long-lived seat hits. An unpinned sidecar will desync seats
that were snapshotted on the previous build.

## Non-goals

- A Go rewrite of pi-ai's OAuth and provider matrix.
- Adopting omp, `omp --mode rpc`, or `omp acp` as the harness.
- Pulling in pi-natives or the coding agent's shell, search, AST, and PTY tools.
- Teaching pi-agent-core to write logs.
- Per-agent JSONL, for mnemo or for resume.
- Using SessionManager's journal as the spool.
- Moving YTT off Claudia.
- Folding Claudia's registry and seat broker into this repo before the fleet is running on the sidecar.
- Treating `OMP_AUTH_BROKER_URL` as a substitute for the seat broker. One stores tokens. The other parents seats.

## Open questions

- Which `@oh-my-pi/pi-agent-core` / pi-ai release is the pin? 18.2.11 is what main was on 2026-09-23, not a chosen pin.
- What is the IPC transport between Claudia's new backend and the sidecar (unix socket, stdio, HTTP)? The verbs are decided. The wire is not.
- Where does `events-YYYY-MM-DD.log` live?
- An event whose own timestamp falls on a day the shim has already closed: append to the closed file (and skip compression if that happens), drop it, or write it onto the live day with the original timestamp inside the record?
- `ditto --hfsCompression` or applesauce? Both produce the same decmpfs result. One of them has to be the one mnemo calls.
- Does the first slice move fleet Grok onto `xai-oauth`, or do Claude / Codex / Cursor move first while the Grok CLI stays until model ids and billing are mapped?
- Does Anthropic bill this OAuth client against included Pro/Max limits or as extra usage? The library cannot answer that.
- How do the existing CLI logins become omp credentials? "Each provider needs an omp login" is the constraint. The migration path is not chosen.
- Which jevons readers must understand the dated log in the first slice so fail-closed resume, inspect, and idle reaping stay honest, and which wait for the later fold? Idle reaping already ignores a non-Claude provider with no JSONLPath. That skip is safe only while nobody treats "no Claude JSONL" as "this seat is idle".
- mnemo's watch is specified (every file in the directory, older dates first). The mnemo change itself is not in this repo, and this design does not name the patch.
