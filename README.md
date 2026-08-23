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

**Second-user path (docs only):** brew install → optional config → open
the web UI → adopt a session → direct an agent. Same-machine browser use
needs no device pairing. Phone/tablet pairing is a separate optional step
(see [Pair a device](#pair-a-device)).

### 1. Prerequisites

Jevons runs its overseer and workers as **Grok** agents, so the
[Grok CLI](https://docs.x.ai/) is a hard prerequisite: install it and
sign in (`grok login`, or set `XAI_API_KEY`) so `grok` is on your `PATH`
(or at `~/.grok/bin/grok`). Without it, jevonsd starts and serves the UI
but the overseer cannot launch — it will tell you so in the chat.

### 2. Install the daemon

```bash
brew install marcelocantos/tap/jevons
brew services start jevons   # always-on launchd service
```

Confirm the service is listening:

```bash
lsof -iTCP:13705 -sTCP:LISTEN
open http://localhost:13705/
```

Or download a binary from the
[latest release](https://github.com/marcelocantos/jevons/releases/latest)
(macOS arm64, Linux x86_64, Linux arm64), or build from source:

```bash
git clone https://github.com/marcelocantos/jevons.git
cd jevons
make jevonsd
```

Requires Go 1.26+ and a C compiler (CGo is needed for SQLite). The
released binary embeds the web UI (no repo checkout required after
install).

### 3. Optional config

No config file is required — defaults work on a clean machine. To
personalize, write `~/.jevons/config.yaml` (see [Configuration](#configuration)).

### 4. First conversation (adopt + direct)

Talk to Jevons in the web chat. It adopts, spawns, monitors, and directs
Grok Build agents on your behalf (thread model). Spend is metered in
real time with an automated clamp-down if a fleet runaway starts.

Everything is plain language — Jevons is the overseer, and it routes your
request to the right agent (or answers directly):

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
- **Direct an adopted or spawned agent** — *"Tell the myrepo agent to fix
  the failing test and summarise the diff."* Idle processes are reaped and
  rehydrated automatically; you never lose the conversation.
- **Watch spend** — *"How much have I spent today?"* Spend also streams
  live in the UI, with an automatic clamp-down if a fleet runs away.

Threads are durable: Jevons stops idle agent processes to save money and
rehydrates them the moment you direct them again — you never lose a
conversation across restarts.

## Usage

```bash
# Start the coordinator (if not using brew services)
jevonsd --port 13705 --workdir ~/projects

# Browser chat (primary UI)
open http://localhost:13705/
```

### Pair a device

Remote phone/tablet clients reach jevonsd through a
[pigeon](https://github.com/marcelocantos/pigeon) QUIC relay (default
bind is loopback-only — the daemon is not opened to the LAN). Same-Mac
browser use does **not** need this step.

#### Auth model (URL + token)

| Piece | Role |
|---|---|
| **Relay URL** | HTTPS base of *your* pigeon relay (WebTransport entry `GET /pigeon`) |
| **Bearer token** | Optional shared secret on the relay. When set, backends must present it to **register**. Mint it yourself; never message the author for one. |
| **Pairing credential** | Separate E2E layer (`PairingArtifact` / `~/.jevons/credential.json`). The relay forwards ciphertext only. |

| Surface | Env / flag | Notes |
|---|---|---|
| Pigeon relay process | `PIGEON_TOKEN` | If **unset**, registration is open (anyone can register a backend). Prefer setting a token you mint. |
| `jevonsd` | `--relay-token` or `TERN_TOKEN` | Pass the **same** value you set as `PIGEON_TOKEN` on the relay. |

`TERN_TOKEN` is a historical env name on the Jevons side; it is **not** a
private author secret and is **not** issued by messaging the project
owner. You choose the string when you start the relay.

#### Scope: self-host only (🎯T156)

Device pair for a second user is **self-host-only**. There is **no**
published free-tier / self-serve multi-tenant pigeon that a stranger can
mint without their own infrastructure. The hostname
`https://carrier-pigeon.fly.dev` appears in flag help and historical
examples as the *shape* of a relay URL; it is the author's private Fly
app (not a public free tier) and not something you obtain a token for by
contacting the author.

#### Self-host a relay (copy-paste from public pigeon docs)

Public steps live in the pigeon README sections **Running the Relay
Server** and **Configuration**
(`https://github.com/marcelocantos/pigeon#running-the-relay-server`,
`https://github.com/marcelocantos/pigeon#configuration`) covering
`PIGEON_TOKEN`, `--cert` / `--key`, and Fly `fly.toml` + `Dockerfile`.
Minimal local path:

```bash
git clone https://github.com/marcelocantos/pigeon.git
cd pigeon
go build -o pigeon ./cmd/pigeon

# Mint your own token — do not ask the author for TERN_TOKEN / PIGEON_TOKEN.
export PIGEON_TOKEN="$(openssl rand -hex 32)"
export TERN_TOKEN="$PIGEON_TOKEN"   # same value for jevonsd

# Dev: self-signed cert (or pass production --cert / --key PEM files).
PORT=4433 ./pigeon
# Production TLS example from pigeon docs:
#   ./pigeon --cert cert.pem --key key.pem
# Optional: deploy *your own* Fly app from pigeon fly.toml + Dockerfile
# (set fly secrets set PIGEON_TOKEN=… yourself). Not carrier-pigeon.
```

Health check once listening: `curl -k https://127.0.0.1:4433/health`
(or your public URL without `-k` when using real certs). Point
`--relay` at that base URL (e.g. `https://relay.example.com` or a
tunnelled `https://…`).

#### Pair and run jevonsd against *your* relay

1. Mint a pairing artifact and QR (writes `~/.jevons/credential.json`
   and prints the artifact):

   ```bash
   export TERN_TOKEN="$PIGEON_TOKEN"   # if not already exported
   jevonsd --pair <device-instance-id> \
     --relay https://YOUR-RELAY-HOST \
     --relay-token "$TERN_TOKEN"
   ```

2. Scan the QR with the **Jevon** iOS app (source under `ios/`; build with
   `make ios` + Xcode — there is no App Store / TestFlight binary yet).
   The app UI prompts you to scan the QR from `jevonsd`.
3. Start the daemon with the same relay URL + token so the paired channel
   stays registered:

   ```bash
   jevonsd --relay https://YOUR-RELAY-HOST \
     --relay-token "$TERN_TOKEN" \
     --instance-id <stable-id>
   ```

Out-of-band server records (e.g. from `pigeon pair`) can be ingested with
`jevonsd --add-credential <path-to-server-record.json>`.

**Honest residual (🎯T47 / 🎯T14):** polished one-shot onboarding
(`jevons --init`, App Store install, public multi-tenant free-tier relay
for strangers) is not shipped. Browser-on-the-same-Mac is the supported
docs-only path today; device pairing works for developers who can build
the iOS client and **self-host a pigeon relay** (steps above — no author
messaging for tokens).

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
provider: ""             # default agent backend for the WHOLE fleet — overseer, workers,
                         # asides, tasks: grok | claude | … ("" = JEVONS_PROVIDER or grok).
                         # `claude` is exercised end-to-end by `make test-journey PROVIDER=claude`;
                         # see agents-guide.md "Running the whole fleet on Claude" for what differs.
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
  --bind              Listen interface (default 127.0.0.1 — loopback only)
  --port              Listen port (default 13705)
  --workdir           Default working directory for workers (default ".")
  --model             Default model for workers
  --provider          Default agent backend (grok, claude, …; empty = config/env/grok)
  --jevons-model      Model for the overseer (default: same as --model)
  --relay             Pigeon relay URL (e.g. https://carrier-pigeon.fly.dev)
  --relay-token       Relay bearer token (or TERN_TOKEN env)
  --instance-id       Stable relay instance id (reconnect without re-pair)
  --pair              Mint pairing QR/artifact for a peer instance id (needs --relay; exits)
  --add-credential    Ingest a server-side PairingRecord JSON file (exits)
  --tls               Enable mTLS on the HTTP listener
  --set-xai-key       Prompt and store an xAI API key in the Keychain (exits)
  --set-openai-key    Prompt and store an OpenAI API key in the Keychain (exits)
  --debug             Enable debug logging
  --version           Print version and exit
  --help-agent        Print agent guide and exit
```

## Data

jevonsd stores its data in `~/.jevons/`:

| Path | Purpose |
|---|---|
| `config.yaml` | Optional structured config (identity, paths, models) |
| `jevons.db` | SQLite database (transcript, workers, raw logs) |
| `usage.db` | Token-spend accounting (cost clamp-down) |
| `budget.json` | Spend budgets and clamp-down thresholds |
| `agents.json` / `threads.json` | Agent registry and durable thread store |
| `credential.json` | Server-side pigeon pairing record (single device) |
| `chatlog/<overseer>.jsonl` | Durable append-only conversation journal — replayed on reconnect so no conversation is ever lost |
| `jevons/` | Jevons working directory and generated instructions |

On startup jevonsd stamps `jevonsmcp` on each seat's `AgentDef.MCPServers`.
It does not write provider HOME configs (`~/.grok/config.toml`,
`~/.claude.json`, and the rest).

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
