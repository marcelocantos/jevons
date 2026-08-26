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

- **`cmd/jevonsd`** — the daemon. HTTP/WS server, web/React asset serving,
  mTLS device provisioning, cost wiring, CLI flags.
- **`ui/`** — the **product** cockpit (Vite + React 19, 🎯T540). Daily
  `:13705` `GET /` serves `ui/dist` built from committed HEAD (🎯T540.2 /
  T553.1). Process owner is launchd KeepAlive `com.marcelocantos.jevonsd`
  (🎯T553.3). React document probe is LaunchAgent `com.marcelocantos.jevons-ui`
  (StartInterval — not a second jevonsd; 🎯T540.4). `make ui-dev` is opt-in
  HMR, not a standing agent. This is where owner-visible UI work lands.
- **`web/`** — **deprecated reference** vanilla cockpit on `:13706`,
  KeepAlive LaunchAgent `com.marcelocantos.jevons-ui-vanilla` (UI-only:
  static `web/` + reverse-proxy to `:13705`; no second `~/.jevons`
  writer). Keep for parity oracles; do not grow product behaviour here.
  Not deleted (separate target). Dist is not `go:embed` (🎯T360);
  brew/pristine without `make ui-build` cannot serve daily React.
- **`ios/Jevon`** — thin client: wraps the daily URL in a WKWebView
  and routes transport over a paired [pigeon](https://github.com/marcelocantos/pigeon)
  QUIC relay (QR pairing artifact → credentials; 🎯T14.1). Daily
  `:13705` is React after cutover.
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
[../STABILITY.md](../STABILITY.md). `jevonsmcp` is attached on seats
Jevons creates via Claudia (`AgentDef.MCPServers`). It is not written
into provider HOME configs (`~/.claude.json`, `~/.cursor/mcp.json`,
`~/.codex/config.toml`, `~/.grok/config.toml`) and isolates do not
write `state_dir/mcp` either. Owner-map HTTP MCP is proxied on
loopback; those URLs are stamped on the same `MCPServers` list (🎯T520).

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

**React mux (🎯T537.1 / T537.1.3).** The product cockpit talks `/ws/mux`.
Each `transcript:{name}` channel is one windowed CQRS stream: the client
names a half-open coalesced window (`[lo, hi)`; `0` is exclusive EOF;
negatives only when following). Dual addressing: opaque event `id` for
identity/append/dedup, dense 1-based `index` for windows. The server
delivers coalesced events the client does not have (connect is `[-30, 0)`
plus a 100-prose halo) and live token appends on the same channel.
First paint reads a byte tail from EOF and folds only that — it does
not Replay the whole journal to mint dense indexes. Page-up is
`before` + `limit` within the loaded tail, not a guessed id range.
Leaving live freezes the window so EOF growth does not slide the view.
Vanilla `/ws/chat` stays a compat shim until cutover (🎯T505).

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

**Typed fleet envelopes (🎯T509).** Load-bearing agent-to-agent messages
open at line 1 with a fenced `jevons` block of `jevons:` slots wrapping
English payload. Schema and enums live in `internal/envelope`. The
daemon validates on `deliverByName` and `chat_wire`: a claimed
load-bearing kind with missing slots is flagged, not silently passed.
Existing classifiers (T31 oracle, T194 daily-path, T386 FALSE-GREEN,
T176 status language) read envelope fields when present and fall back
to prose only for unenveloped messages. Status-ping/ack chatter is
deduped and rate-capped by kind. The cockpit (`web/scripts/jevons_envelope.js`)
paints a compact header, not a raw fence dump. YAML front matter is not
this format.

### One deliver path in the fleet layer (🎯T309.3)

The **send** op above bottoms out in a single implementation,
`mcpserver.deliverByName(name, text, origin, interrupt)`. Everything that
delivers a message to an agent goes through it:

| Caller | Route |
|---|---|
| MCP `jevons_agent_send` | `sendToAgentAs(actor)` → `deliverByNameAs` (origin pinned to `agent`; 🎯T321) |
| `POST /api/agents/{name}/send` | `DeliverAgentMessageAs` → `deliverByName` (owner surface) |
| worker reply notify, worker-idle, daemon-restarted, fleet health | `notify` / `emit*` → `deliverByName` (owner surface) |

Two arms, one contract. A **fleet** name resolves through the registry
(rehydrating a stopped agent) and delivers with the 🎯T111.1 busy
semantics. The **overseer** name resolves to the owner-chat delivery seam
(`SetOverseerDeliver` → `server.DeliverToOverseerAs`), which owns
journalling, owner bubbles, and the notify queue. `mcpserver` does not
import `server`, so the two share the `origin` wire strings rather than a
type; `main` adapts.

Before this, the overseer had a **privileged wire** — a bare
`notifyJevon(text)` injection set from `main` — and which peers an agent
could reach depended on which API it held rather than on the hierarchy.
That split carried real bugs: `jevons_agent_send` addressed to the
overseer went straight at its process, bypassing the journal and the
queue-on-busy retry (the 🎯T62 drop), and `notify` discarded a worker's
terminal report with a log line when no notify func was set.

**Authorization is decided on the path, by lineage** (`deliver_policy.go`,
pure): report up and direct down are always allowed, peer messaging is
allowed on purpose (🎯T309 acceptance 3), and `origin: owner` — which
paints an owner bubble — may be asserted only by the owner surface. MCP
`jevons_agent_send` pins `agent` origin, so the fleet has no way to speak
in the owner's voice. **Per-caller actor (🎯T321):** `jevons_agent_send`
takes an explicit `actor` (same shape as `jevons_agent_kill`) and the MCP
path calls `deliverByNameAs` with it, so `AuthorizeDeliver` is exercised
against a named caller and denials log `actor` + `relation`. **Residual
(impersonation):** the shared MCP HTTP transport still cannot
cryptographically name the calling fleet agent (`transcript.GetID` is the
overseer session); `actor` is self-attested, matching kill's trust model.

`jevons_thread_direct` is **not** a residual deliver variant. It is the
*synchronous* request/reply op: it subscribes to the agent's event stream
**before** sending and assembles the whole reply, an ordering that
`internal/fleet/reply.go` documents as load-bearing (🎯T286 — subscribing
after the send loses the front of the reply). Folding it into the
fire-and-forget path would reintroduce that truncation, so it stays a
distinct operation rather than a shim.

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
`bind_addr` / `--bind` deliberately widens it; remote devices use a
**self-hosted** [pigeon](https://github.com/marcelocantos/pigeon) relay
(URL + optional bearer token: pigeon `PIGEON_TOKEN` ↔ jevonsd
`--relay-token` / `TERN_TOKEN` — mint yourself; no author-issued free
tier, 🎯T156), not LAN exposure; mTLS CA + QR device provisioning
available when enabled; origin-safe WebSockets and CSRF guards on
mutating routes (🎯T38); cost clamp-down bypass hardening (🎯T36.1).
Not yet: mTLS off by default; workers and the overseer still run
permissions-bypassed (worker execution gating is 🎯T8.3 post-MVP);
stranger-ready device onboarding (App Store binary + `jevons --init` +
public multi-tenant free-tier relay) is still 🎯T14 / residual of 🎯T47.
Treat a default install as single-trusted-operator, single-machine.

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
