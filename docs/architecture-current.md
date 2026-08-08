# Jevons — Current Architecture

Living document. This is the one page that describes the system as built;
when it drifts from the code, fix it here first (🎯T42 keeps it honest).
Governance (who decides what) lives in [charter.md](charter.md); the design
philosophy behind the agent model lives in [vision-v2.md](vision-v2.md);
interaction-surface stability contracts live in [../STABILITY.md](../STABILITY.md).

## What Jevons is

Jevons is a coordinator for a community of coding agents under a single
CEO agent. The human owner talks to one agent — *Jevons*, the CEO — which
adopts, spawns, monitors, and directs worker agents on the owner's behalf.
The daemon (`jevonsd`) is infrastructure, not an agent: it hosts the chat
surfaces, the MCP tools the CEO drives, durable state, and cost governance.

## Components

```
  browser ──ws──┐
                ├──►  jevonsd (Go) ──spawns/manages──►  Grok agents (claudia)
  iOS app ──────┘        │
   (WKWebView            ├── in-process MCP server  ◄── tool calls from agents
    over pigeon           ├── durable state (~/.jevons)
    QUIC relay)          └── cost clamp-down (usage.db, budget.json)
```

- **`cmd/jevonsd`** — the daemon. HTTP/WS server, dev-mode web serving,
  mTLS device provisioning, cost wiring, CLI flags.
- **`web/`** — the canonical UI. Self-contained chat client (markdown,
  xterm.js terminal viewer, agent status), served by jevonsd. Transport is
  abstracted (`web/scripts/transport.js`): WebSocket in the browser, a
  native bridge inside the iOS WKWebView.
- **`ios/Jevon`** — thin client: wraps the bundled web UI in a WKWebView
  and routes transport over a paired [pigeon](https://github.com/marcelocantos/pigeon)
  QUIC relay (QR pairing artifact → credentials; 🎯T14.1).
- **[claudia](https://github.com/marcelocantos/claudia)** — the agent
  harness library: process spawn, Grok ACP (session/new, session/load,
  session/prompt), Task one-shots, tmux-backed fleets, the agent registry.
  Default agent provider is Grok via claudia; selection is pluggable (🎯T148).
- **[pigeon](https://github.com/marcelocantos/pigeon)** — relay, pairing,
  and crypto primitives (PairingArtifact / PairingHost / CredentialStore).

## The agent model

Three concepts, one durable spine:

- **Thread** (`internal/thread`, `internal/butler`) — the durable unit:
  a provider conversation (session id) + workdir + status. Threads survive
  restarts; **the process is a disposable cache** — spun up on `Direct`,
  reaped when idle (GC every ~2 min), rehydrated via session resume.
  Lifecycle: `Adopt` (observe-only, non-invasive) → `TakeOver`
  (two-writer-guarded) → `Spawn` / `Direct` / `Remove`. Invariants with
  enforcement: no silent-fail direct; never drive a session another
  process holds; malformed durable state is a hard error, never a silent
  reset.
- **Overseer ("jevons")** — a persistent named Grok agent that is the CEO.
  Identity and persona come from structured config (`~/.jevons/config.yaml`
  + built-in `persona.md` template; 🎯T44) — no owner-specific identity in
  compiled code. External ecosystem providers are declared under the same
  file's `providers:` list (id, transport launch|connect, enable, params)
  with runtime registry state in `~/.jevons/providers/registry.db` (🎯T27.2;
  reloadable via `provider.ConfigManager` without a full daemon restart;
  process supervise/reconcile is 🎯T27.3). Communication is fire-and-forget:
  `jevons_agent_send`
  returns immediately; worker replies and completions arrive as async
  notifications pushed into the overseer's conversation.
- **jwork** (`internal/mcpserver/jwork.go`) — the ephemeral primitive: a
  fresh worker runs one self-contained task to completion and returns the
  result (recursion capped at depth 3).

The durable-thread path is the only agent lifecycle (🎯T41 removed the
legacy manager/session MCP generation). The CEO drives everything through
the in-process **MCP server** (`internal/mcpserver`, `jevons_*` tools +
`jwork`); the tool list and stability grades are in
[../STABILITY.md](../STABILITY.md).

## The conversation surface (🎯T309.2)

Conversation is **one agent-addressed API family**. Three operations, all
resolved by agent name, with the overseer as just another addressable
agent — there is no conversation capability the owner chat wire holds
exclusively:

| Op | Surface |
|---|---|
| transcript | `GET /api/agents/{name}/transcript`, and the `inspect_subscribe` history frame over `/ws/chat` |
| live | `inspect_subscribe` → `agent_transcript` `kind=live` frames |
| send | `POST /api/agents/{name}/send` (`origin: owner\|agent`), MCP `jevons_agent_send` |

Turns share one shape (`turn_number`, `role`, `text`) whatever their
origin; the payload names that origin in `source`:

- `session` — a provider session transcript (fleet agents), read through
  `internal/transcript`.
- `chatlog` — the owner chat journal, which is the overseer's durable
  conversation record. Addressing the overseer by name returns its turns
  from there; the 🎯T124 refusal ("overseer uses main chat") is gone.

**Transport residual** — what remains overseer-specific is transport and
durability, not capability:

- `GET /api/history` and the `/ws/chat` replay-on-connect are a paging /
  transport **compat shim** over the same journal, kept until the
  transport cutover (🎯T10.6) and the single-widget UI cutover (🎯T309.1)
  move main chat onto the family.
- A `/ws/chat` client subscribed to the overseer sees both the main chat
  line and the `agent_transcript` live frame for one event; de-duping
  belongs to the single widget (🎯T309.1).
- Conversation **control** ops (rewind, interrupt) are separate from the
  conversation family on both sides: the overseer's ride `/ws/chat`
  control frames, fleet agents' ride MCP (`jevons_transcript_rewind`,
  `jevons_agent_stop`) and `POST /api/agents/engagement/stop`.

Server↔client event normalization stays in one layer:
`internal/server/chat_wire.go` ↔ `web/scripts/chat_events.js`. Overseer
live frames carry chat-wire lines verbatim, so 🎯T240 silent-stream
suppression and 🎯T223 stream ids are inherited, never re-implemented.

## The provider seam (🎯T45)

Jevons defaults to Grok via claudia with a pluggable selection surface (🎯T148); the
boundary is explicit so a second backend inherits the whole control
plane rather than forking it:

- **Lifecycle** — `butler.Fleet` (`internal/butler/butler.go`):
  Launch / Send / Alive / Stop / Remove. The butler owns policy
  (rehydrate, reap, two-writer guard); the Fleet owns mechanism.
  Production implementation: `internal/fleet` over claudia's registry.
- **Cost enforcement (L3)** — `cost.EnforcerArgs.Actions`
  (PauseWorker / KillWorker / StopFleet / KillSwitch): the policy
  engine never touches tmux or the registry directly;
  `cmd/jevonsd/cost.go`'s `fleetActions` is the claudia binding.
- **Cost collection (L1)** — `cost.CollectorArgs` (ProjectsRoot +
  Attribute): the store/monitor/enforcer are format-agnostic; only the
  collector's JSONL parser knows Grok's layout, and its root comes from
  config (`sessions_dir`), not compiled paths.
- **Resume safety** — claudia v0.18.0's `Config.RequireResume` +
  `AgentDef.Materialized` make failed session loads fail closed; jevons
  relies on the registry passing this automatically.

claudia and pigeon are published, documented dependencies:
[claudia](https://github.com/marcelocantos/claudia) (agent harness —
README, agents-guide, STABILITY) and
[pigeon](https://github.com/marcelocantos/pigeon) (relay/pairing/crypto
— README with trust model). A clean checkout builds with only public
module fetches.

## Cost governance

`internal/cost` (🎯T36, honesty 🎯T137): L1 collector tails every active
Grok session JSONL (registered or not) into `usage.db`; L2 monitor
computes rolling burn rates per worker/fleet/global; L3 enforcer
escalates warn → throttle → pause → kill with irreversibility guards
(minimum-evidence before kill, sustained-breach confirmation, protected
workers, dead-man's switch). Budgets are editable in
`~/.jevons/budget.json`.

**Accounting modes** (`budget.json`):

| Mode | When | Enforcement on USD | UI $ |
|---|---|---|---|
| `list_price` (default) | paid API / real marginal $ | full ladder + hard ceiling | billable $ |
| `subscription` | SuperGrok / flat plan | never pause/kill on API-eq $ | labeled "est / not billed" |
| `disabled: true` | full opt-out | no enforcer | ticker hidden; `/api/cost` reports disabled |

Fleet pause never stops the overseer (🎯T139).

**On-demand multi-harness usage (🎯T163):** `internal/harnessusage` +
`cmd/harness-usage` produce observational reports for Claude Code, Grok,
Codex, and Cursor (local JSONL/SQLite scrape; optional live-API probe
with soft degrade). Not an enforcement plane — see
[harness-usage.md](harness-usage.md).

## Persistence

| Path | Contents | Survives restart |
|---|---|---|
| `~/.jevons/agents.json` | claudia agent registry (names, session ids) | yes |
| `~/.jevons/threads.json` | durable thread store (atomic write-and-rename) | yes |
| `~/.jevons/usage.db` | token-spend events (SQLite) | yes |
| `~/.jevons/budget.json` | clamp-down policy | yes |
| `~/.jevons/credential.json` | pigeon pairing (single device) | yes |
| `~/.jevons/chatlog/<overseer>.jsonl` | jevons-owned append-only conversation journal (🎯T30.1) | yes |
| `~/.grok/sessions/…/chat_history.jsonl` | provider transcripts | yes (provider-owned) |
| process state, voice FSM, broadcast fan-out | in-memory | no — by design |

UI history replays from the jevons-owned chatlog so conversation display
does not depend on the provider's private store (🎯T30.1).

## Security posture (honest)

Built: default bind is **loopback-only** (`127.0.0.1` / 🎯T6) unless
`bind_addr` / `--bind` deliberately widens it; remote devices use the
pigeon relay, not LAN exposure; mTLS CA + QR device provisioning
available when enabled; origin-safe WebSockets and CSRF guards on
mutating routes (🎯T38); cost clamp-down bypass hardening (🎯T36.1).
Not yet: mTLS off by default; workers and the overseer still run
permissions-bypassed (worker execution gating is 🎯T8.3 post-MVP);
stranger-ready device onboarding (App Store binary + `jevons --init`) is
still 🎯T14. Treat a default install as single-trusted-operator,
single-machine.

## Voice

De-emphasized. 🎯T37 decided **no-go** on xAI Voice Agent Builder as the
primary stack (decision note: [analysis/voice-agent-builder-eval.md](analysis/voice-agent-builder-eval.md)).
Dictation is Wispr Flow into the text field. When full-duplex resumes,
prefer client→Grok Speech-to-Speech with ephemeral tokens (🎯T22 shape),
not the Builder console/telephony product. Dormant machinery remains in
`/ws/voice`, `internal/server/voice*.go`, and `ios/…/VoiceManager.swift`.

## Package map

| Package | Role |
|---|---|
| `internal/server` | HTTP/WS hub, chat wire normalization, voice bridge, dev server |
| `internal/butler` / `internal/thread` | CEO orchestration over durable threads |
| `internal/fleet` | claudia-backed `butler.Fleet` implementation (the provider seam) |
| `internal/mcpserver` | the `jevons_*` MCP tool surface + `jwork` |
| `internal/cost` | spend collection, monitoring, clamp-down |
| `internal/auth` | mTLS CA, cert issue/verify |
| `internal/discovery` | Grok session scanning + liveness |
| `internal/transcript` | provider transcript read/truncate/fork |
| `internal/cli` | version, provider constant, embedded agent guide |

## Glossary

- **ACP** — Agent Client Protocol: the JSON-RPC stdio protocol claudia
  speaks to the Grok CLI (`session/new`, `session/load`, `session/prompt`).
- **claudia** — the agent-harness library (spawn, ACP, registry, tmux fleets).
- **pigeon** — relay/pairing/crypto library; carries iOS ↔ jevonsd traffic.
- **Thread** — durable unit of work (conversation + workdir + status);
  outlives its process.
- **Agent** — a named registry entry (name → provider session) that can be
  launched; **session** — the provider-side conversation a thread wraps.
- **Overseer / Jevons / butler / CEO** — the same distinguished agent: the
  one the owner talks to.
- **Task vs Session (claudia)** — Task: one-shot subprocess, runs to
  completion; Session: long-lived ACP conversation.
- **jwork** — MCP tool dispatching an ephemeral Task worker.
- **bullseye** — the intent ledger (`bullseye.yaml`, 🎯Tn targets).
- **mnemo** — external MCP server indexing session transcripts; owns memory
  (the charter's "Jevons never remembers").
