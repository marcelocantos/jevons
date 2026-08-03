# Jevons ([why?](#about-the-name))

Your one-stop shop to manage your AI life.

Today that means remote control for [Grok](https://docs.x.ai/) coding
agents: talk to a coordinator that manages Grok workers — from a
browser, or from your phone. The charter is wider: one cockpit
that connects you to every AI service you run — status at a glance, events
that need your attention, and an agent that can act on any of them. How
Jevons governs — the owner/CEO model, arbitration on attested evidence, and
risk-graded decision rights — is defined in [docs/charter.md](docs/charter.md).

## How it works

```
  web UI / iOS  ──WebSocket──►  jevonsd  ──spawns──►  Jevons (Grok)
                                        ──manages──►  workers  (Grok)
                                   MCP ◄─────────────┘ (tool calls)
```

**jevonsd** is the coordinator daemon. It runs *Jevons* — a Grok
session that receives your messages and decides whether to answer directly or
delegate coding tasks to *worker* sessions. Jevons manages workers via
an in-process MCP server (no separate binary needed). Multiple clients can
connect simultaneously; messages and responses are broadcast to all.

The **web UI** (served by jevonsd) is the canonical surface; the iOS app
wraps the same UI in a WKWebView over a paired QUIC relay.

## Install

Jevons runs its overseer and workers as **Grok** agents, so the
[Grok CLI](https://docs.x.ai/) is a prerequisite: install it and sign in
(`grok login`, or set `XAI_API_KEY`) so `grok` is on your `PATH` (or at
`~/.grok/bin/grok`). Without it, jevonsd starts and serves the UI but the
overseer cannot launch — it will tell you so.

```bash
brew install marcelocantos/tap/jevons
brew services start jevons   # always-on launchd service
```

Open the web chat at [http://localhost:13705/](http://localhost:13705/).

Or download a binary from the
[latest release](https://github.com/marcelocantos/jevons/releases/latest)
(macOS arm64, Linux x86_64, Linux arm64), or build from source:

```bash
git clone https://github.com/marcelocantos/jevons.git
cd jevons
make jevonsd
```

Requires Go 1.26+ and a C compiler (CGo is needed for SQLite).

## Usage

```bash
# Start the coordinator (if not using brew services)
jevonsd --port 13705 --workdir ~/projects

# Browser chat (primary UI)
open http://localhost:13705/
```

Talk to Jevons in the web chat. It adopts, spawns, monitors, and directs
Grok Build agents on your behalf (thread model). Spend is metered in
real time with an automated clamp-down if a fleet runaway starts.

### Talking to Jevons

Everything is plain language — Jevons is the overseer, and it routes your
request to the right agent (or answers directly). A first conversation:

- **Ask what it can do** — *"What are you working on right now?"* Jevons
  answers directly; simple questions never spawn an agent.
- **Delegate work** — *"Start an agent in `~/work/myrepo` and have it run
  the tests and summarise failures."* Jevons spawns a worker there and
  reports back when it's done (the reply arrives on its own; you don't
  wait or poll).
- **Check status** — *"What's the current work on myrepo?"* or *"List
  your threads."*
- **Adopt a session you already have open** — if you're running Grok in a
  terminal, *"Adopt session `<uuid>`"* brings it under Jevons as a durable
  thread you can direct from the chat. (Stop driving it in the terminal
  first — one writer at a time.)
- **Watch spend** — *"How much have I spent today?"* Spend also streams
  live in the UI, with an automatic clamp-down if a fleet runs away.

Threads are durable: Jevons stops idle agent processes to save money and
rehydrates them the moment you direct them again — you never lose a
conversation across restarts.

### Configuration

jevonsd reads `~/.jevons/config.yaml` (or `--config <path>`); flags
override the file. Everything has a working default — no config file is
required. All fields:

```yaml
owner_name: Ada          # how the overseer refers to you (default: "the owner")
overseer_name: jevons    # the CEO agent's name
bind_addr: 127.0.0.1     # loopback-only by default; remote devices use the pigeon relay
port: 13705
workdir: "."             # default workdir for workers
provider: ""             # default agent backend: grok | claude | … ("" = JEVONS_PROVIDER or grok)
model: ""                # default worker model ("" = provider default)
overseer_model: ""       # "" = same as model
state_dir: ~/.jevons
sessions_dir: ~/.grok/sessions
repos_root: ~/work/github.com
mcp_server_name: jevonsmcp # name of the overseer's MCP server in ~/.grok/config.toml
persona_file: ""         # optional replacement for the built-in persona template
persona_notes: |         # freeform extras appended to the overseer's persona
  sqlpipe is the state-sync repo; route DB questions there.
```

### Flags

```
jevonsd:
  --config            Config file path (default ~/.jevons/config.yaml)
  --bind              Listen interface (default 127.0.0.1)
  --port              Listen port (default 13705)
  --workdir           Default working directory for workers (default ".")
  --model             Default model for workers
  --provider          Default agent backend (grok, claude, …; empty = config/env/grok)
  --jevons-model      Model for the overseer (default: same as --model)
  --debug             Enable debug logging
  --version           Print version and exit
  --help-agent        Print agent guide and exit
```

## Data

jevonsd stores its data in `~/.jevons/`:

| Path | Purpose |
|---|---|
| `jevons.db` | SQLite database (transcript, workers, raw logs) |
| `usage.db` | Token-spend accounting (cost clamp-down) |
| `budget.json` | Spend budgets and clamp-down thresholds |
| `agents.json` / `threads.json` | Agent registry and durable thread store |
| `chatlog/<overseer>.jsonl` | Durable append-only conversation journal — replayed on reconnect so no conversation is ever lost |
| `jevons/` | Jevons working directory and generated instructions |

On startup jevonsd also registers its MCP endpoint user-scoped in
`~/.grok/config.toml` (an `[mcp_servers.<mcp_server_name>]` entry) so the
overseer's management tools attach when the Grok CLI resumes its session
across restarts.

## Agent integration

If you use an agentic coding tool, include
[`agents-guide.md`](agents-guide.md) in your project context for a detailed
reference. You can also run `jevonsd --help-agent` to get the same information.

## About the name

Jevons is named after [Jevons paradox](https://en.wikipedia.org/wiki/Jevons_paradox):
when technological progress makes a resource cheaper to use, total consumption
of that resource tends to *increase* rather than decrease. AI coding assistants
make development dramatically more efficient — so you end up doing more of it,
not less. Jevons leans into this by letting you orchestrate multiple Grok
sessions at once, multiplying the effect — so keep an eye on your AI bill at
the end of the month (Jevons meters it in real time and clamps down on
runaways for you).

## Licence

Apache 2.0 — see [LICENSE](LICENSE).
