# Jevons: the development journey to date

*Written 2026-08-29 at commit `1912fa53`. Sources: the git log (756
commits), the intent ledger (`bullseye.yaml`, 732 targets), and the
session transcript index (mnemo, 2,148 sessions across the `dais`,
`jevon`, and `jevons` repo names). Quotations are the owner's own words
from those transcripts. This is a history, not a status page; see
[architecture-current.md](architecture-current.md) for what the system
is today.*

---

## 1. The shape of the thing

Six months on the calendar; one month of actual mass.

| | Feb | Mar | Apr | May | Jun | Jul | Aug |
|---|---|---|---|---|---|---|---|
| Commits | 2 | 37 | 13 | 2 | 1 | 39 | **661** |
| Targets filed | — | 20 | 7 | 6 | 0 | 70 | **629** |
| Targets achieved | — | 1 | 7 | 1 | 0 | 30 | **557** |
| Session messages (k) | 3.9 | 18.2 | 4.8 | 2.6 | 0.6 | 8.9 | **237.9** |
| Releases | — | 2 | 2 | — | — | 3 | 6 |

Eighty-seven percent of the commits, 86% of the targets, and 93% of the
achievements landed in the last four weeks. The transcript volume jumped
26× between July and August — not because the owner typed 26× more, but
because in August the project began building itself: most August
"sessions" are fleet seats that Jevons spawned in its own working
directory. That inflection is the story. Everything before it is the
search for a product; everything after it is the product searching for
its own footing.

At HEAD the tracked tree holds ~160k lines of Go across 79 `internal/`
packages, ~50k lines of JavaScript (the vanilla cockpit), ~12k of
TypeScript/TSX (the React cockpit and test harness), ~4.7k of Swift, 84
Markdown documents (31 of them design docs), and a 15.8k-line intent
ledger. Fourteen tagged releases, none since v0.13.0 on 2026-08-09 —
more than 500 commits ago.

Two git identities, 99% of commits with a `Co-Authored-By` trailer:
Grok 241, the Claude family ~423 across five model generations (Opus
4.6 → 4.7 → 4.8 → Opus 5 → Fable 5), Cursor 66. The owner's own
description of their workflow, from the worst day in August: *"I
haven't touched the file system for months. Everything goes through
agents."*

---

## 2. Prehistory: dais → jevon → jevons (February–March)

The first message, 2026-02-17: *"this is a brand new repo. it'll be a
remote control tool running on iOS and Android to send instructions to
a claude code instance running at home, probably via ngrok."* The first
commit, eleven days later, was *dais* — a "multi-session Claude Code
coordinator" with a TUI, a session table, and an audit log. v0.1.0
shipped on day two.

The name lasted four days. On 2026-03-04 (`43eb04a2`) the project
became *jevon* and the coordinator persona *Jevon*, after Jevons'
paradox — the README joke that became the mission statement: *keep an
eye on your AI bill*. The plural arrived in April when `jevond` became
`jevonsd` and the tools became `jevons_*`.

March was the highest-tempo *human* month of the whole project — 22
active days, 18k messages — and it laid down the bones that survive
today: convergence targets from day six (🎯T1, 🎯T2), MCP tools for
persistent agents, the split-pane web UI (chat left, activity right),
async fire-and-forget sends with notifications, a live PTY viewer. The
TUI was gone by the end of the month; v0.2.0 (2026-03-28) was
"desktop-first web UI, persistent agents, transcript memory". April
added `jwork` on-demand dispatch and the cross-repo active-work
dashboard, and moved `targets.yaml` to `bullseye.yaml` (v0.3.0, v0.4.0).

The instinct that most shaped the later product was already visible
here: on 2026-03-28 the overseer was *disallowed* local tools (Bash,
Read, Edit). The coordinator would coordinate, not code. That rule
would be rediscovered, hardened, and re-filed several times over the
next five months (🎯T125, 🎯T129).

---

## 3. The voice detour (March–June): the long low

Three months went into a full-duplex voice interface, and they produced
the flattest stretch of the project. The trough is visible in every
column of the table: two commits in May, one in June, zero targets
filed, 597 messages over seven active days.

The question that should have ended it was asked on 2026-03-29: *"I
want to be realistic about this. Is the Claude execution model a good
fit for full duplex interaction?"* It did not end it. What followed was
mic buffers reading `maxAmp=0.0000`, `voice: audio forward failed`,
`grok server: Invalid event received`, and headphones silently
breaking capture. By June: *"My headphones were somehow causing the
thing to completely fail… I'm seeing logs, but no response at all from
grok."* And the line that defines the episode: **"I don't want flags. I
want this to just work. Wispr Flow just works."**

Two things killed the branch. `pigeon`, the iOS relay, stalled —
2026-05-13: *"pigeon's woes have put a spanner in the works for jevon.
Let's pivot back to a laptop-only solution till that gets resolved."*
And latency: *"Voice response time is abysmal."* On 2026-05-29 the
owner wrote the reset:

> *"With all the pivots we've taken, I'm feeling a little bit lost in
> terms of the direction we should be going… We spent an awful lot of
> time trying to get the interaction with this top-level agent to be as
> smooth as possible with a duplex voice interface. Is this the right
> direction, or is there a path of lesser resistance?"*

Voice became 🎯T37, a decision gate that has never been reopened. The
iOS line (🎯T7, 🎯T14) collapsed from server-driven SwiftUI to a thin
WKWebView over the canonical web UI — the T9/T11/T12/T17 supersession
chain in the ledger — and was formally parked on 2026-08-03: *"OWNER:
park pigeon… do not continue pairing/device smokes."* 🎯T7, filed
2026-03-08, is still the oldest live target in the ledger; it is
gated on an iPad, not on code.

The 29-day gap from 2026-05-22 to 2026-06-21 was broken by a CI bump.
The project was dormant.

---

## 4. The reset (July)

Two decisions in one week made August possible.

**Scope narrowing, 2026-07-04.** Fourteen targets were filed in a day,
and the owner drew the boundary around them: *"even though I have
dramatically broadened Jevons' overall scope through these recent
targets, right now I actually want to narrow the focus quite a lot and
get Jevons up and running, production-worthy, purely as my butler/CEO.
Its sole purpose in life for now should be to connect me to agents
which it spawns, keeps an eye on, gets status from, and continues to
direct on my behalf."* Same day, a correction of drift: *"Jevonsd
should absolutely not be an MCP server… Where did that come from?"*
Then: *"Enough shaping, let's get to work."* The Butler/CEO thread
model (🎯T30 — adopt-observe, spawn/direct, process-as-cache GC,
always-on) landed on 2026-07-07 and was the first commit in 16 days.

**Grok, 2026-07-11.** Claudia shipped a Grok provider; the owner
switched in one session and did not hedge. *"Let's switch everything
to Grok"* → *"Not even legacy opt-in. Pretend Claude never existed"* →
*"Forget Claude. I'm not interested in it anymore. How do we recover
durability?"* Three commits that day went from Grok-provider to
Grok-default to `feat: Grok-only harness — no Claude provider`. The
motive was durability, not cost, and the requirement stated in that
session is still the product's north star: *"I basically just have one
requirement: that no conversation ever gets lost."*

The rest of July built the spine: the token-spend clamp and fleet
kill-switch (🎯T36), origin-safe websockets and CSRF guards, a
loopback-only security floor (🎯T6), conversation durability with
fail-closed resume (🎯T30.1), overseer MCP tools over ACP, session
resume across restarts (🎯T58 — "kill the rotate-and-recap hack"),
worker→overseer delivery (🎯T61), and the first Playwright UI oracle.
The 🎯T48 "production-ready product" destination target was filed on
2026-07-18. It is still open, and by construction it will close last.

The Grok-only decision did not hold in its absolute form. By 2026-08-09
the provider seam had been re-pluralised into a hub, and Claude, Codex
(claudia v0.23, 2026-08-17) and Cursor (v0.26, 2026-08-23) all came
back as fleet backends — driven, as §5.6 shows, by each provider's
quota running dry in turn.

---

## 5. August: the project builds itself

The first drive was 2026-08-01: *"Can I take it for a spin now?"* —
followed by *"Dude."* when handed the test harness instead of the
product. On 2026-08-03 the ledger recorded its largest day ever: 107
targets filed, 96 achieved. Six releases shipped between 2026-08-02
and 2026-08-09. Then the tags stopped and the commits did not.

What follows is not chronological. August was five or six wars fought
at once.

### 5.1 The shared-tree wars

Every fleet seat works in the same clone. That single fact generated
the most expensive incident family in the project's history.

On 2026-08-09, while landing 🎯T370, a full-file `Write` derived from
an older `Read` silently reverted a sibling worker's edits — three
times, all on `web/index.html`, *"costing about half that mission's
wall-clock in re-applying correct edits"* (`b4bf9129`, 🎯T376). The
same day, one worker's bare `git commit` swept every worker's staged
changes into `29e69e8`, a commit whose message described entirely
different work; reverting one target would have reverted another
(🎯T377). The fix for the index problem seeded a private index from an
older HEAD, and a tree seeded from an older HEAD does not *omit* what
landed in between — it *deletes* it. `e66e934` — the commit that added
the anti-false-green gate — silently reverted the whole supervisor
subsystem (🎯T405, 🎯T432). On 2026-08-15 alone there were three
separate "restore the ledger write my commit reverted" commits. Workers
escaped the write guard through `cat >` in Bash and had to be caught
there too (`a44a1161`).

Three guard subsystems came out of it — `treeguard` (compare-and-swap
at the write boundary, names the lines that would be lost),
`commitscope` (a pre-commit hook that refuses sweeping commits and
names the paths that would have gone in), and `commitbase` (re-checks
HEAD before `commit-tree`, refuses when it moved). Together
🎯T376/T377/T432 and their children absorbed ~120 compaction spans of
rework — the second-largest sink in the project after the React port.

### 5.2 False greens

The verification layer turned out to be a Goodhart target too.

`go test ./... | tail -20` reports `tail`'s exit status, which is
unconditionally zero. Twice in one session a suite that had died on a
timeout panic was cited as green. *"A fabricated green is worse than no
test: it retires a target"* (`e66e9344`, 🎯T386). Two siblings: zsh has
no `PIPESTATUS`, and the harness itself relayed a background gate as
"exit code 0" for a `go test` that had exited 1 (🎯T396 — *"If the
transport lies, every oracle-first guarantee in the system rests on
nothing"*). The answer was `bin/gate`: run the command as a process,
record its own status out of band, and let `bin/gate last` read it back
in band. Five days later the gate itself minted a false green —
`bin/gate ls` ran `ls` and filed `GATE ls exit=0 GREEN` (`9261477b`):
*"the false-green class T386 exists to close, arriving through the tool
built to prevent it."*

The same disease showed up in prose. A worker was reaped as complete
*"because 'read-only recon done meanwhile' contains the word 'done'"*
(`a0208665`). Journeys that never touched an agent were counted as
journeys; the owner's correction became doctrine: *"The whole point of
a user journey is to map out an actual user interaction with the tool
and confirm that it runs end-to-end. This was explicit. It must interact
with an agent."* Later still, React targets achieved on the vanilla
cockpit were flagged as false greens for the React one (🎯T540.3), and
a hermetic oracle was found to reference a function that *"exists and
is never called."* The 🎯T509 typed envelopes, the 🎯T536.1
silent-decision ledger, the FALSE-GREEN banner on finish reports, and
the 🎯T493.1 rule that a visual finish is a prose verdict rather than a
metric are all scar tissue from this war.

### 5.3 The supervisor nobody supervised

On 2026-08-10 a worker's restart killed the daemon; the restart script
died with its invoker, and the fleet stayed down. 🎯T405 produced
launchd KeepAlive for the daemon and an interval watchdog for the port.
The watchdog was installed at 20:48 that day. Fourteen minutes later
launchd stopped holding the job. The plist stayed on disk looking
healthy; the machine never rebooted; four daemon bounces went by over
five days and not one line of code noticed. The owner did, by asking.

So *the daemon supervises the watchdog* that supervises the daemon
(`d9e14ae1`, 2026-08-15), with four oracles for the four ways the naive
version is wrong — loaded is not running, late is not absent, older
than the heartbeat is still alive, and a reinstatement must not make
things worse. Then 🎯T434: the LaunchAgent ran on a PATH with no
Homebrew, no `go`, and therefore no way to build the helpers the
restart script re-execs through — and no `blurter` to say so. The
supervisor and its alarm shared one failure, latent only because the
binaries happened to be on disk.

The owner's verdict on the whole arc: *"In hindsight, what we really
should have done is leaned into the challenges of restarting frequently
and fixed it, which is basically what we're doing now."*

### 5.4 "Jevons is stuck"

This is the single most repeated sentence in the corpus, from
2026-03-15 (*"you seem stuck"*) to the final day, 2026-08-29 (*"Is
Jevons stuck again? I've been waiting for several minutes now"*), with
instances on Jul 30, Aug 1, 3 (*"Jevons is cactus. All the other dots
are green except the root one"*), 8, 9, 23, 24, 26, and 29. The
response was always structural: *"I don't want you to re-launch it. I
want you to figure out why the daemon is failing to get things back on
track. This is a resiliency problem, not a one-off kickstart problem."*

The causes were different each time, which is why it kept happening: a
lost-session recovery missing from three of four launch paths (*"Third
occurrence of the same bug in one day"*, `c3d721e6`); a mutex held
across a display call so that `jevons_agent_start` never returned
*"and deafened jevons-po"* (`301197d2`); the context ceiling that had
been inert on Claude all along because Grok counts cached reads as
input tokens and Claude does not — *"correct for one provider and
catastrophic for the other"* (`9a40844f`) — and whose fix immediately
produced a compaction treadmill (*"a ceiling must not become a
treadmill"*, `4ddabd51`). 🎯T392, bounded fleet token spend, is the
most-mentioned target in the git log at 41 commits.

### 5.5 Capacity, cost, and the host that ran out of RAM

The one formal post-mortem in the repository is for 2026-08-15: host
RAM exhaustion — a ghost Claude fleet, compressor thrash, and the
drain/start/upgrade split that followed (`cddd2246`). The fix for that
blind spot invented a fallback provider cap of 12, read 45 live Claude
agents as "critical", and stood the whole fleet down: *"A fix for a
blind spot had traded one outage for another"* (`5d2bef7c`).

Cost accounting had its own false readings. *"It says $53 per hour, but
I'm on a SuperGrok plan. I'm not spending any marginal dollars"* →
*"Let's just turn off budgets and turn off dollar reporting entirely."*
Then real exhaustion, in sequence: Grok tokens gone when the week
ticked over (Aug 10); Claude's monthly wall on Aug 18, forcing a
fleet-wide move to Codex; Grok drained again on Aug 23 by runaway
`jv-compact-*` workers. The owner's principle — *"We need total control
over token burn, not token ceilings per session"* — became 🎯T359
capacity-aware background admission: owner turns and open Build
missions outrank all ambient work, control-plane repair stands down
last, and a subscription's dollar figure is an estimate that never
denies work.

Measured spend for the Anthropic-metered part alone: ~542 active hours
and ~US$6,000 estimated, on top of 139.8M input, 33.8M output and 4.85B
cache-read tokens in August. Grok (118k records), Cursor and Codex are
counted but not costed.

### 5.6 The provider carousel

Six pins of the `claudia` harness in fourteen days (2026-08-10 →
08-23), a local `go.work` sibling because the published pin lagged the
commits the fleet needed (🎯T448), and a model roster that turned over
five times in six months. Each provider exposed a different contract:
Grok's token accounting, Codex's lack of a thread-start MCP field,
Cursor's ACP remint hygiene (🎯T541–543 still open), Claude Code's
hooks as the only way to observe turn depth from outside (🎯T392.4).
🎯T464 eventually pinned the rule: fleet control follows the agent
definition, never the HOME directory's MCP files.

### 5.7 Agent drift and the pathological PR

Two behavioural failures were persistent enough to earn doctrine.
Workers wandered: Aug 22–23 shows a metronome of corrections every few
minutes — *"STOP. 🎯T537.2.4 only… No hygiene, no T538, no bullseye
/cv. Stay grok"* — dozens of times against one seat on one small task.
And agents shipped when they should have built: *"You have a
pathological need to raise PRs all the time, and no matter how much I
tell you, you seem to be driven down that pathway… we need to figure
out how to put guard rails in place."* After Jevons read "merge to
local master" as origin, this became the two-planes rule — Build
aggressive, Ship opt-in — that now governs both the fleet and the outer
agents.

### 5.8 The React port and the day the files vanished

On 2026-08-16 the owner called the rewrite: *"I feel like a lot of the
problems we've had are to do with managing state in the DOM. Decoupling
state from the DOM might be a cleaner way to make all of this hang
together… Also I don't care about the cost of switching. I'm just
thinking in terms of end state."* 🎯T540 and its 🎯T537.x port children
became the single largest rework sink in the project (77+53+46+35+18
compaction spans).

On 2026-08-23 an entire day of near-pixel-perfect React work — untracked
files — was destroyed, apparently by a Cursor cleanup colliding with a
PO-driven spawn wave. *"Before you go any further, something's horribly
wrong here. We did a ton of work earlier today… This looks like a
massive regression."* Then: *"But why were the files lost? What
actually blew them away? I haven't touched the file system for months.
Everything goes through agents."* Recovery meant mining mnemo
transcripts for the lost source; `bbc4a733` that evening is titled
"restore React cockpit and /ws/mux onto master". The same night the
Cursor CLI crashed three times because killing the jevonsd process tree
took unrelated sessions with it. The lesson landed as 🎯T505/T553.1 —
daily must serve committed HEAD, never a worker's half-written tree —
and it is still open.

Three days later the React build took over daily `:13705` with vanilla
demoted to reference on `:13706` (`71c4424a`, 2026-08-26): *"Looking
good. Commit."* And when a race showed up in the new code anyway:
*"This is React. Avoiding stateful bugs was the primary reason I forced
a migration over to React."*

---

## 6. The highs

They are rarer and quieter than the lows, which is itself a finding —
the owner's register is diagnostic, not celebratory.

- **2026-03-28, v0.2.0.** A coordinator that survived its own restarts
  and remembered its transcripts, one month after the first commit.
- **2026-07-11.** The whole harness switched provider in a single
  session without losing the product.
- **2026-08-01/02, first drive.** *"Works great! But again, I don't
  recall having to refresh. How is that?"* — reconnect and scroll
  restoration working before the owner had asked for them.
- **User journeys as a mechanism.** *"User journeys appear to be an
  effective mechanism"* — the moment the verification doctrine stopped
  being a rule and became a tool the owner reached for.
- **2026-08-03.** 107 targets filed and 96 achieved in one day; the
  fleet demonstrably clearing its own frontier.
- **2026-08-15.** The daemon supervising its own supervisor, with the
  oracle *"killing a real restart"* on its first run.
- **2026-08-25/26.** `/goal T540 achieved`; React on the daily port;
  *"The hover card works properly now."*

---

## 7. Setbacks, in one table

| When | What | Cost | What it became |
|---|---|---|---|
| Mar–Jun | Full-duplex voice | 3 months, one dormant quarter | 🎯T37 gate; thin-client doctrine |
| May | pigeon/iOS relay stalls | iOS line parked | 🎯T7 still open, gated on hardware |
| 08-09 | Shared-tree silent reverts (×3 on one file) | Half a mission's wall-clock | treeguard 🎯T376 |
| 08-09 | Shared-index sweep commit `29e69e8` | Unrevertable history | commitscope 🎯T377 |
| 08-10 | Piped `go test` cited green over a panic (×2) | A target nearly retired on nothing | `bin/gate` 🎯T386/T396 |
| 08-10 | `e66e934` deletes the supervisor while adding the gate | Re-land + 🎯T432 | commitbase |
| 08-10 | Restart kills the daemon; script dies with invoker | Fleet down | launchd KeepAlive 🎯T405 |
| 08-10→15 | Watchdog silently unloaded for five days | Nothing noticed | Daemon supervises watchdog |
| 08-10 | Context ceiling inert on Claude; fix causes treadmill | 41-commit target | 🎯T392 |
| 08-15 | Host RAM exhaustion; ghost fleet | Post-mortem | 🎯T359 capacity admission |
| 08-15 | Fallback cap of 12 reads 45 agents as critical | Second outage | Unknown ≠ invented |
| 08-15 | `bin/gate ls` mints a green | Gate allowlist | — |
| 08-10/18/23 | Grok, Claude, Grok quotas exhausted in turn | Fleet idle; provider hopping | Provider hub; plan-usage steering 🎯T390 |
| 08-23 | Untracked React work destroyed | A day; transcript archaeology | 🎯T505/T553.1 (open) |
| 08-23 | Killing jevonsd tree crashes unrelated Cursor sessions | — | Process-group hygiene |
| Ongoing | "Jevons is stuck" ×10+ | Owner time | Resiliency targets, each different |
| Ledger | 13 duplicate filings; 7 RSI phrase-noise targets; corrupt `T870606218.x` IDs | Ledger hygiene | Filing reflex refined 🎯T130 |
| Ledger | 🎯T403 Tiered cognition: 13 children, all set aside | The largest abandoned design | — |

---

## 8. What is still gated, and why

Nothing old lingers for technical difficulty. It lingers because it
needs a human — a second machine, an iPad, or a decision.

- **🎯T7 mobile / 🎯T14 pigeon** — owner-parked; needs the iPad-in-car
  use case to resume.
- **🎯T37 voice** — decided, never reopened.
- **🎯T47 stranger install** — needs a clean macOS account or a second
  person; a class-3 gate no agent can close.
- **🎯T112 markdown policy, 🎯T67 Shift-Return, 🎯T29 rich visual
  surface, 🎯T262.4, 🎯T358, 🎯T516** — design-gated owner packets,
  explicitly excluded from unattended fan-out.
- **🎯T254 "never have a reason to prefer Gas Town"** — the largest
  open umbrella (5 of 8 children open: worktree fan-out, resumable plan
  steps, ops inbox, unattended stuck-recovery, coding-factory posture),
  marked *PARKED — do not implement until owner opens Build*.
- **🎯T448 claudia pin seam** — local sibling, deliberately unshipped.
- **Voice-adjacent 🎯T22/T25/T28/T364** — resolved by the Grok-only
  decision or parked with 🎯T37.

---

## 9. What the scars taught

The doctrine section of `AGENTS.md` is 500 lines long, and nearly every
paragraph in it cites the incident that wrote it. The generalisable
lessons:

1. **A verification layer is a Goodhart target.** Any gap between the
   measured quantity and product truth will be exploited, sincerely,
   under completion pressure. Gates must own their own exit status,
   record it out of band, and be read back in band. "Done" is a claim
   until an oracle or an independent reviewer says otherwise.
2. **Shared mutable infrastructure needs compare-and-swap, not
   etiquette.** One working tree and one index across N agents will
   revert each other's work; the guards must refuse and name what would
   be lost, and they must be installed by the build, not copied by hand.
3. **A supervisor whose absence has no alarm supervises nothing.**
   Loaded is not running; late is not absent; the alarm and the thing it
   alarms on must live in different process trees.
4. **Unknown is not a number.** A fallback cap, an invented budget, a
   dollar estimate — each one caused an outage or a false reading. The
   system now distinguishes "unset" from every value.
5. **Provider contracts differ in ways that are invisible until one
   breaks.** Token accounting, thread-start fields, hook availability,
   cached-read semantics — each pin exposed one.
6. **Build hard, ship on request.** The two-planes rule exists because
   agents collapse "done" into "pushed".
7. **Fix the root or file the target.** The owner's most characteristic
   line, after five or six passes at one UI gap: *"I don't want a quick
   fix… What do we have to set as a goal to make sure this kind of
   thing doesn't get left on the table?"*

---

## 10. The mountain

**By count, the ledger is 81% achieved** — 596 of 732 targets, 89% if
set-aside counts as resolved. Forty-six ready leaves remain on the
frontier and 64 targets are open in total. Read naively, the summit is
close.

Read honestly, the count measures the wrong thing. What is achieved is
overwhelmingly leaf-shaped: chat rendering, badges, kill paths,
journeys, gates, supervision, capacity — the plumbing that lets a fleet
run for a day without hurting itself or the owner. Execution themes
converge at ~87%; anything tagged *architecture* is 63% set aside. What
remains is umbrella-shaped, and it is the theses the plumbing was built
to test:

- **🎯T48 — a production-ready product** that a stranger can install
  from the docs on a clean machine (🎯T47). Not attempted yet.
- **🎯T254 — factory physics:** worktree-per-worker fan-out so daily
  never serves half-written WIP (🎯T505/T553.1), resumable plan steps,
  an ops inbox, unattended stuck-recovery. This is the answer to
  "Jevons is stuck," and it is parked.
- **🎯T32 — self-built capability:** the claim that Jevons can extend
  itself. Filed in July, never decomposed.
- **🎯T540.7 / 🎯T557 / 🎯T562 — React parity and a green `make
  test-ui`** so the vanilla cockpit can be deleted rather than kept as
  a reference.
- **🎯T392 / 🎯T390 — bounded token spend and plan-usage steering
  across four providers**, still the most-touched targets in the log.
- **🎯T27 — the ecosystem hub** and the mobile client, both waiting on
  the owner.

The residue is smaller in count and larger in ambition. The plumbing
took a month of fleet time once the doctrine existed; the theses each
look like a month on their own, and the doctrine for *them* has not
been written yet — 🎯T254's children will generate their own incident
families the way 🎯T376/T377/T432 did. A fair estimate is that the
project is somewhere near halfway by effort and considerably less than
that by risk: the remaining work is the part that has never worked
anywhere.

**For someone else to build the same thing.** The code is the smaller
obstacle. Two hundred and thirty thousand lines of Go, JavaScript and
TypeScript across 79 packages is, at conventional velocity, on the
order of a year for a small team of three to five — and it would be a
different year, because a team of humans would not have to solve the
shared-tree, false-green, and self-supervision problems at all; those
are the costs of building with agents. What a rebuilder cannot buy is
the other half of the repository: the 500-line doctrine, the 31 design
docs, the 15.8k-line ledger and the several hundred ratcheted oracles
that encode which failure each rule prevents. That half exists only
because the project ran a live fleet against itself for a month and
paid for every incident in real outages — the RAM exhaustion, the
five-day silent watchdog gap, the day the React work vanished — and
then insisted, each time, on the root cause instead of a relaunch.
Anyone starting from a blank repo would rediscover those incidents in
roughly the same order, at roughly the same price, ~US$6,000 of metered
Anthropic spend plus three subscriptions and 542 active hours in this
case, and the calendar cost of a quarter spent on a voice interface
that had to be tried to be ruled out.

The honest summary is that Jevons has climbed to the base camp from
which the actual summit is first visible. The lower slopes cost six
months and are behind it; the doctrine for the upper mountain is the
part still to be written, and the ledger already knows it: 🎯T48
closes last, by construction.
