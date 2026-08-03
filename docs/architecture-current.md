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
  Its instructions are currently embedded in `cmd/jevonsd/main.go`
  (externalizing them is 🎯T44). Communication is fire-and-forget:
  `jevons_agent_send` returns immediately; worker replies and completions
  arrive as async notifications pushed into the overseer's conversation.
- **jwork** (`internal/mcpserver/jwork.go`) — the ephemeral primitive: a
  fresh worker runs one self-contained task to completion and returns the
  result (recursion capped at depth 3).

A legacy third generation (`internal/manager` + session MCP tools) is
being deleted (🎯T41).

The CEO drives everything through the in-process **MCP server**
(`internal/mcpserver`, `jevons_*` tools + `jwork`); the tool list and
stability grades are in [../STABILITY.md](../STABILITY.md).

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

`internal/cost` (🎯T36): L1 collector tails every active Grok session
JSONL (registered or not) into `usage.db`; L2 monitor computes rolling
burn rates per worker/fleet/global; L3 enforcer escalates
warn → throttle → pause → kill with irreversibility guards
(minimum-evidence before kill, sustained-breach confirmation, protected
workers, dead-man's switch). Budgets are hot-editable in
`~/.jevons/budget.json`.

## Persistence

| Path | Contents | Survives restart |
|---|---|---|
| `~/.jevons/agents.json` | claudia agent registry (names, session ids) | yes |
| `~/.jevons/threads.json` | durable thread store (atomic write-and-rename) | yes |
| `~/.jevons/usage.db` | token-spend events (SQLite) | yes |
| `~/.jevons/budget.json` | clamp-down policy | yes |
| `~/.jevons/credential.json` | pigeon pairing (single device) | yes |
| `~/.grok/sessions/…/chat_history.jsonl` | provider transcripts (source of truth for history) | yes (provider-owned) |
| process state, voice FSM, broadcast fan-out | in-memory | no — by design |

A jevons-owned append-only transcript (so history display never depends
on the provider's private store) is 🎯T30.1.

## Security posture (honest)

Built: mTLS CA + QR device provisioning; origin-safe WebSockets and CSRF
guards on mutating routes (🎯T38); cost clamp-down bypass hardening
(🎯T36.1). Not yet: default bind is all-interfaces with mTLS off;
workers and the overseer run permissions-bypassed; the charter's
risk-graded permission model is not enforced (🎯T5, 🎯T6). Treat a
default install as single-trusted-operator, single-machine.

## Voice

De-emphasized pending the 🎯T37 decision (adopt xAI Voice Agent Builder
vs DIY). The web mic was removed — dictation happens via Wispr Flow into
the text field. Dormant machinery remains in `/ws/voice`,
`internal/server/voice*.go`, and `ios/…/VoiceManager.swift`.

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
