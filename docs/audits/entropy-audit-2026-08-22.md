# Entropy audit — jevons — 2026-08-22

Full-mode audit (entropy + hygiene). Frozen against the snapshot below.
Citations are `path:line` on committed trees (`git show <sha>:path`).
Working-tree WIP was treated as user-owned and was not mixed into
production conclusions except where named as residue.

## Executive summary

- **Snapshot:** `/Users/marcelo/work/github.com/marcelocantos/jevons`
  - Branch: `master` (ahead of `origin/master` by 559 at start; 561 by
    end of evidence gathering)
  - **Initial HEAD (frozen):** `cb06b532b6cc36ffb1f8f1568cb539c22c0c3416`
    (`fix(web): pin inspect transcript to live end like main chat`)
  - **HEAD at report write:** `429e106b36e7ab2d28dff685df571050a316ec61`
    — two unrelated worker commits landed during the audit:
    `50328854` `fix(chat): one owner-send path — sendToNamedAgentAs`
    and `429e106b` `fix(web): delete renderAgentInspect dump`
  - **Initial dirty state:** heavily dirty shared clone. Porcelain at
    start included modified `AGENTS.md`, `Makefile`, `agents-guide.md`,
    `bullseye.yaml` (MM), `cmd/jevonsd/main.go`,
    `docs/architecture-current.md`, many `internal/mcpserver` /
    `internal/server` / `web/scripts` paths, staged deletions, and
    untracked role/gate/journey files. **14 paths were already staged.**
    None of that work was discarded, unstaged, or restaged.
  - Date: 2026-08-22
- **Scope:** committed Go daemon (`cmd/`, `internal/`), canonical web UI
  (`web/`), Makefile/CI/release, architecture and stability docs, iOS
  thin-client sources (not vendored C/C++). See exclusions.
- **Headline mechanism:** new product behaviour accretes onto three
  unbounded hubs — `internal/mcpserver` (MCP surface *and* staff/fleet
  policy), `internal/server` + a ~10k-line inline script in
  `web/index.html`, and `cmd/jevonsd` as composition root — while the
  documents that claim to catalogue the cut (`docs/architecture-current.md`
  package map, `STABILITY.md` MCP table) describe a much smaller 2026-07
  system. Dual-path defects (T504, T371, T372) are the recurring symptom
  of that shape, not isolated UI bugs. 🎯T516 already names the watch
  that would catch this; it is still `identified` / needs-owner.
- **Highest-consequence findings:** ENT-001 (god hubs), ENT-002
  (STABILITY is not the live MCP catalogue), ENT-003 (architecture-current
  cut vs 69 packages + two live lifecycles), ENT-004 (CI is a subset of
  the declared product net).
- **Unverified residue:** `make test` / journeys / Playwright were not
  run (dirty shared clone; Universe B needs a live provider). No
  clone-detector. `govulncheck` not installed. `staticcheck` /
  `golangci-lint` exist on the host but are not in CI and were not run
  against this dirty tree. Runtime drift between `~/.jevons/agents.json`
  and `threads.json` was not inspected (operator state, not the repo).
  Dirty WIP (roles T511, measured-tree gate, viewport census) may already
  move some of the instruction-corpus duplication; it is not claimed as
  landed.

## Scope and exclusions

**In scope**

- `cmd/`, `internal/` production Go (non-test) and the tests that
  document intended seams
- `web/index.html`, `web/scripts/*.js` (product UI; tests as oracles)
- `Makefile`, `.github/workflows/{ci,release}.yml`, `go.mod`/`go.sum`
- `docs/architecture-current.md`, `STABILITY.md`, `AGENTS.md`,
  `agents-guide.md`, `internal/config/persona.md`, design packet
  `docs/design/architecture-entropy-watch.md`, 🎯T42 / 🎯T516 in
  `bullseye.yaml`
- `ios/Jevon/**/*.swift` and `ios/project.yml` (what the app actually
  compiles), not the vendored C/C++

**Named exclusions (not silent omissions)**

| Tree | Why skipped |
|---|---|
| `ios/Vendor/lua-5.1.5/`, `ios/Vendor/sqlpipe/`, `ios/Vendor/sqldeep/` | vendored third-party; still *named* as a compile-time cost in ENT-006 |
| `scripts/browser-loop-test/` | 177-file Playwright/lab harness, not the product |
| `scripts/chat-ui-test/*.png`, `testdata/`, `internal/*/testdata/` | fixtures / golden |
| `bin/`, `build/`, root `jevonsd`, `sqlite_mcp_server.db` | gitignored build/runtime artefacts |
| `docs/design/*` except the entropy-watch packet and where a finding
  needs a counterevidence check | historical / in-flight design, not the cut |
| Working-tree uncommitted role files under `internal/config/roles/` | user-owned WIP |

## Commands run

All from repo root. Tool versions: `go 1.26.4 darwin/arm64`,
`git 2.55.0`, `node v26.7.0` (present, unused for a suite run),
`Python 3.13.0`. Analyzers on PATH but **not** invoked: `staticcheck`
(`~/go/bin/staticcheck`), `golangci-lint` (`/opt/homebrew/bin/golangci-lint`).
Not on PATH: `jscpd`, `deadcode`, `govulncheck`.

| Command | Path | Exit | What it decided | Limitation |
|---|---|---|---|---|
| `git rev-parse --abbrev-ref HEAD && git rev-parse HEAD && git status --porcelain=v1 -b` | snapshot | 0 | branch `master`, initial HEAD `cb06b532…`, dirty + 14 staged | snapshot only |
| `go version` | aux | 0 | `go1.26.4` | — |
| `git ls-files` + `wc -l` on HEAD blobs for hubs | aux | 0 | loc: `web/index.html` 11731; `cmd/jevonsd/main.go` 1814; `internal/server/chat.go` 1471; `internal/mcpserver/agents.go` 1130 | loc ≠ quality |
| Python import-graph over `git show HEAD:…` production `.go` | aux | 0 | no internal-package SCCs; fan-out mcpserver 46, cmd/jevonsd 34, server 32; loc mcpserver 22959/82 files, server 14766/56 | top-level `internal/x` only; nested packages folded |
| `git grep mcp.NewTool("` on HEAD `internal/mcpserver` vs `STABILITY.md` table | shipped-docs vs code | 0 | 52 tools in code, 18 in the STABILITY table; `jevons_reload_views` table-only | table grep does not read prose “removed” sections |
| `git log --since='90 days ago' --name-only` | history | 0 | `web/index.html` 83 commits; `cmd/jevonsd/main.go` 56; `internal/mcpserver/{mcpserver,agents}.go` 44+43 | co-change, not proof of a bug |
| `git rev-list --count HEAD` / `--since='30 days ago'` | history | 0 | 664 total; 586 in 30 days (at initial HEAD) | volume locates pressure |
| `/Users/marcelo/.claude/skills/hygiene/hygiene_check.py` | hygiene | 1 | `FileNotFoundError: …/hygiene.yaml` | expected; posture undeclared, not drift |
| `make test` / `make test-web` / `make test-ui` / `make test-journey` | shipped net | **not run** | — | dirty shared clone; journeys need a signed-in provider |
| `go test ./...` | shipped Go net | **not run** | — | same; CI is the standing remote Go gate |
| `staticcheck` / `golangci-lint` / `jscpd` | aux | **not run** | — | not declared in CI; dirty tree would mix WIP |

Shipped-path vs auxiliary is marked above. Metrics located hubs; they are
not themselves findings.

## Observed architecture

### Declared (agrees with code)

- One daemon (`jevonsd`) hosts HTTP/WS, in-process MCP, durable state
  under `~/.jevons`, and cost clamp-down.
  `docs/architecture-current.md:19-26`, `:28-42`.
- Provider seam: `butler.Fleet` policy vs `internal/fleet` mechanism
  (`internal/butler/butler.go:29-51`).
- One fleet deliver implementation: `deliverByName` /
  `deliverByNameAs` (`internal/mcpserver/deliver_by_name.go:15-24`).
  HTTP send, MCP `jevons_agent_send`, and notify/idle all claim this door.
  A commit during this audit (`50328854`) further collapsed an owner-send
  fork onto `sendToNamedAgentAs` — direction matches the declared rule.
- Chat wire is a two-sided protocol copy:
  `internal/server/chat_wire.go` ↔ `web/scripts/chat_events.js`, called
  out in `AGENTS.md` and exercised by `make test-web`.
- Cost is layered: L1 collector / L2 monitor / L3 enforcer
  (`internal/cost/cost.go:8-25`); `internal/planusage` is subscription
  remaining via claudia, not a second ledger; `internal/harnessusage` is
  observational (`docs/architecture-current.md:230-234`).
- Security posture is honest: loopback default, pigeon for devices,
  permissions-bypassed workers, single-operator
  (`docs/architecture-current.md:252-266`).
- No import cycles among `internal/*` packages (command evidence).
- `mcpserver` does not import `internal/server`; `main` adapts
  (architecture-current conversation-surface section; observed in
  `cmd/jevonsd` import list).
- Shared-clone guards exist and are load-bearing: treeguard (T376),
  commitscope (T377), commitbase (T432), `bin/gate` (T386/T396),
  docratchet, watchdog (T405/T434).

### Observed, inferred from code (not in the package map)

Sixty-nine top-level `internal/*` packages at HEAD. Load-bearing ones
the package map omits include: `config`, `provider`, `capacity`, `rsi`,
`audit`, `research`, `supervise`, `handover`, `envelope`, `planusage`,
`chatlog`, `eventlog`, `staffops`, `converge`, `ctxcap`, `gate`,
`poproactive`, `targetfile`, `sync`, `writconf`, plus many small
classifiers. `cmd/` has 19 packages (daemon + guards + `harness-usage`
+ loop-test binaries).

`internal/mcpserver` is not “the tool surface + jwork”. At HEAD it
registers 52 tools (50 `jevons_*`/`jwork` + `self_test.{run,list}`)
across RSI coach, residual RSI mint, T357 auditor, T356 research,
sentinel, staff-ops, capacity, ideas, writs, plan usage, owner-gate,
target-file, migrate, reconnect, logs, screenshots, fleet-intent, and
both **agent_*** and **thread_*** families.

### Contradictions

1. `docs/architecture-current.md:72-76` — “The durable-thread path is
   the only agent lifecycle” and “the tool list and stability grades are
   in STABILITY.md”. Code has two live MCP families (`jevons_agent_*` in
   `internal/mcpserver/agents.go`, `jevons_thread_*` in
   `internal/mcpserver/threads.go:19-22`) and dual durable files
   (`agents.json` + `threads.json`, architecture-current `:240-241`).
   Thread records dual-write into the agent registry on Launch
   (`internal/thread/thread.go:46-47`, 🎯T114). STABILITY’s MCP table
   (`STABILITY.md:51-70`) lists 18 tools including a ghost
   `jevons_reload_views` and omits 35 live tools.
2. `docs/architecture-current.md:34-36` — iOS is a thin WKWebView
   wrapper. `ios/project.yml:27-41` still compiles Lua 5.1.5, sqlpipe,
   and sqldeep; `ContentView.swift:14-29` still has three UI generations
   (WebUIView / ServerView / purpose-built fallback);
   `Connection.swift:419-420` still loads Lua scripts.
   `STABILITY.md:191-192` says client-side Lua “is not yet wired”.
3. `docs/architecture-current.md:277-289` package map lists 9 packages;
   the tree has 69. T42 (`bullseye.yaml` `T42`, achieved 2026-07-18)
   promised one current-architecture page a contributor can read instead
   of unioning README + agents-guide + STABILITY + bullseye. The page is
   maintained at the spine (last touch `3ea88f2d` 2026-08-21) but the
   map was not grown with the tree. T42’s ratchet
   (`scripts/docratchet/t47_install_doc_test.go:42-61`) only forbids a
   few stale phrases and requires four truth-pass markers — it cannot
   see a missing package.
4. 🎯T516 / `docs/design/architecture-entropy-watch.md:82` treat
   `hygiene.yaml` as existing floors. The file is absent.

### Unknown intent (owner)

- Whether `jevons_thread_*` remains a product path or compatibility
  residue now that T114 dual-write makes every thread an agent.
- Whether iOS Lua/sqlpipe/native views are accepted cold-start residue
  or scheduled deletion (T10 is `set_aside`; T9/T11/T12 Lua membrane is
  “partly superseded”).
- Whether 🎯T516 is accepted (sibling architecture coach) or parked.
- Whether CI is *intentionally* Go-only (journeys cannot run on GitHub
  without a provider) or an accidental gap for Node hermetics /
  Playwright.

```
                    ┌──────── web/index.html (inline ~10k) + web/scripts ────────┐
browser/iOS WKWebView ──WS/HTTP──► internal/server (14.8k loc, fan-out 32)
                                      │
cmd/jevonsd (composition, fan-out 34)─┼── internal/mcpserver (23.0k loc, fan-out 46, 52 tools)
                                      │         │
                                      │         ├── butler/thread  ──┐
                                      │         └── fleet/claudia ──┴─ agents.json + threads.json
                                      ├── cost / planusage / harnessusage
                                      ├── rsi / audit / research / sentinel / staffops / capacity
                                      └── supervise / provider / config
```

Dependency direction that *holds*: leaf classifiers (`envelope`,
`relayroute`, `silentresponse`, `agenterr`, …) do not import hubs;
`mcpserver` does not import `server`. Direction that *is* the entropy:
almost every new staff or fleet concern grows `mcpserver`’s import set
and tool list, and almost every cockpit concern grows the inline
`index.html` script or `conversation_widget.js` (2024 lines).

## Dimension vector

| Dimension | State | Evidence summary | Change from baseline |
|---|---|---|---|
| Architecture topology | concern | No internal cycles; butler/fleet and deliverByName seams hold. Package map names 9 of 69 packages. mcpserver fan-out 46. | n/a (first full entropy report) |
| Redundancy / sources of truth | concern | Dual agent/thread stores and MCP families (T114 dual-write). STABILITY table vs 52 tools. Three instruction corpora. iOS Lua vs WKWebView. T309/T372/T504 closed earlier UI forks; T516 still open. | n/a |
| Change amplification | concern | `web/index.html` 83 commits / 90d, 11731 lines with a 9971-line inline script. mcpserver 23k loc. 586 commits in 30 days. T376/T377 exist *because* the hubs are shared-clone hot files. | n/a |
| Local code quality | concern | Linear, well-commented Go in the leaves; hubs are long files (`chat.go` 1471, `main.go` 1814, `agents.go` 1130, `idle_nudge.go` >1500 on disk). Not a style finding — the length is the hub. | n/a |
| Correctness / verification | concern | Dense hermetic Go (412 `*_test.go`) + Node (51 `*_test.js`) + Playwright + Universe-B journeys locally (`Makefile:349`). CI runs only `go test` (`.github/workflows/ci.yml:35-38`). Gate/docratchet/treeguard are real. Journeys not exercised this run. | n/a |
| Security / dependencies | concern | Honest single-operator posture. No dependabot, no `govulncheck`, no secret-scan job. Workers permissions-bypassed (declared). `go.mod` pins claudia v0.24.0 / pigeon v0.19.0. | n/a |
| Build / release / operations | healthy | `make all` builds daemon + guards; watchdog + supervised restart (T405/T434); Homebrew release workflow; `bin/gate` records status. Release CI is Go-only like CI. | n/a |
| Documentation / governance | concern | T42 page exists and is edited; map and STABILITY lag. T516 packet exists, not accepted. `hygiene.yaml` absent. `docs/todo.md` still tracked. Instruction doctrine split across persona / agents-guide / fleet_brief / AGENTS.md. | n/a |

Do not aggregate these into a score.

## Findings

### ENT-001: Product change accretes onto three unbounded hubs

- **Priority:** P1
- **Dimensions:** Architecture topology; Change amplification; Local code quality
- **Status:** observed fact
- **Evidence:**
  - HEAD loc / fan-out (import-graph command): `internal/mcpserver` 22959 lines, 82 production files, 46 internal packages imported; `internal/server` 14766 / 56 / 32; `cmd/jevonsd` 34 internal imports, `main.go` 1814 lines.
  - `web/index.html` 11731 lines at HEAD; `<style>` `:17-1495`; one inline `<script>` `:1759` through end (~9971 lines) after 50+ extracted `web/scripts/*.js` includes.
  - 90-day churn: `web/index.html` 83 commits, `cmd/jevonsd/main.go` 56, `internal/mcpserver/mcpserver.go` 44, `agents.go` 43, `internal/server/chat.go` 33.
  - Design packet already names these hubs:
    `docs/design/architecture-entropy-watch.md:80`.
  - Motivating incident T504 (achieved): two display models,
    `foldDisplayEvent` vs `applyLiveDisplayFrame` — cited in
    `bullseye.yaml` T516 context (`:12421` family).
- **Mechanism:** a new fleet/staff behaviour has a cheap home in
  `mcpserver` (register another `AddTool`); a new cockpit behaviour has
  a cheap home in the inline script or `conversation_widget.js`. The
  next change to “how an agent is addressed” or “how a bubble grows”
  must be understood in both the extracted module *and* the hub, which
  is how T504/T371/T372 forks formed. Shared-clone contention (T376
  guarded paths include `web/index.html`) is the same mechanism on the
  working tree.
- **Blast radius:** every fleet tool, every owner-visible chat paint,
  every daemon boot wiring. Future cleanup/re-cut of MCP or cockpit
  touches these files and collides with in-flight workers.
- **Counterevidence checked:** fan-out at `cmd/jevonsd` is a legitimate
  composition root. `deliverByName` and `butler.Fleet` are real seams.
  T309/T372 extracted `ConversationWidget` (`web/scripts/conversation_widget.js:1-15`
  claims one widget, `wireComposer:false` gone). Leaves under
  `internal/{envelope,relayroute,capacity,gate,…}` are directional and
  small. Extraction of scripts is underway — the remaining 10k inline
  lines are the residue, not a claim that nothing was extracted.
- **Smallest coherent remediation:** (1) freeze new `mcp.NewTool` and new
  inline `index.html` behaviour behind an explicit “this is a hub edit”
  rule / ratchet; (2) move staff loops (rsi/audit/research/sentinel) to
  `cmd/jevonsd` wiring + small packages that `mcpserver` only registers;
  (3) finish extracting the inline script by concern (send, history,
  inspect hydrate) until `index.html` is markup + CSS + bootstraps.
  Do not rewrite mcpserver in one PR.
- **Verification:** architecture test: `internal/mcpserver` production
  import count of *other* `internal/*` packages is ratcheted (today 46);
  `web/index.html` non-markup line count ratcheted; docratchet that the
  package map lists every `internal/*` directory with >500 production
  loc.
- **Ratchet candidate:** Go test in `scripts/docratchet` (or
  `internal/audit` sensor, T516) computing fan-out and `index.html`
  inline-script lines from source. Floor = today’s numbers; movement
  requires a deliberate commit.

### ENT-002: STABILITY.md is not the live MCP catalogue

- **Priority:** P1
- **Dimensions:** Redundancy / sources of truth; Documentation / governance
- **Status:** observed fact
- **Evidence:**
  - `STABILITY.md:13` — “Snapshot as of v0.13.0 … last full table base
    was v0.5.0”.
  - `STABILITY.md:51-70` table lists 18 tools, including
    `jevons_reload_views` (`:70`).
  - `git grep mcp.NewTool("` on HEAD `internal/mcpserver/*.go`
    (non-test): 52 tools. In code, not in table (35):
    `jevons_agent_migrate`, `jevons_agent_report_read`,
    `jevons_audit_{configure,cycle,report,residue,status}`,
    `jevons_capacity_status`, `jevons_event_push`, `jevons_fleet_intent`,
    `jevons_{idea_capture,idea_list,idea_triage}`, `jevons_logs_tail`,
    `jevons_mcp_reconnect`, `jevons_owner_gate`, `jevons_plan_usage`,
    `jevons_research_*` (5), `jevons_rsi_coach_*` (3), `jevons_rsi_cycle`,
    `jevons_rsi_disposition`, `jevons_screenshot`,
    `jevons_security_status`, `jevons_sentinel_cycle`,
    `jevons_staff_ops_cycle`, `jevons_target_file`, `jevons_writ_exec`,
    `self_test.{list,run}`.
  - In table, not in code: `jevons_reload_views` (Lua-era;
    `docs/lua-membrane.md:79` still describes it).
  - architecture-current `:74-76` tells a contributor to trust STABILITY
    for the tool list.
- **Mechanism:** two catalogues for one public surface. An agent or
  human reading STABILITY will call a removed tool or miss the product
  path (coach vs residual `jevons_rsi_cycle`). Stability grades
  (“Fluid”) are not applied to the tools that actually shipped after
  v0.5.0.
- **Blast radius:** every MCP client (overseer, workers, external),
  STABILITY’s 1.0 contract (`STABILITY.md:3-8`), T42 “one page instead
  of unioning”.
- **Counterevidence checked:** pre-1.0, Fluid is allowed to grow. The
  “were removed” prose for session tools (`STABILITY.md:46-49`) is
  honest. The *table* is still presented as the catalogue. Dual RSI
  tools are documented in the `jevons_rsi_cycle` description as residual
  — but only in the tool schema, not in STABILITY.
- **Smallest coherent remediation:** regenerate the MCP table from
  `mcp.NewTool("` registrations (or a `Tools()` inventory) in
  docratchet. Delete or mark `jevons_reload_views` as removed. Split
  residual tools (`jevons_rsi_cycle`) into an explicit residual section.
- **Verification:** test that every `mcp.NewTool` name appears in
  STABILITY and every STABILITY current-table name exists as
  `NewTool`. Fail on either direction.
- **Ratchet candidate:** `scripts/docratchet` test, same shape as
  T509 envelope / T47 install markers. Source-derived denominator.

### ENT-003: architecture-current’s cut is a 9-package map over a 69-package tree

- **Priority:** P1
- **Dimensions:** Architecture topology; Documentation / governance
- **Status:** observed fact
- **Evidence:**
  - Package map `docs/architecture-current.md:277-289` — 9 packages.
  - `git ls-files 'internal/*/*.go'` → 69 top-level `internal/*`
    packages at HEAD.
  - Lifecycle claim `docs/architecture-current.md:72-73` vs live
    `jevons_agent_start` (`internal/mcpserver/agents.go:57-74` at
    working tree; registration exists on HEAD) and
    `SetButler` thread tools (`internal/mcpserver/threads.go:19-22`).
  - T42 achieved 2026-07-18 (`bullseye.yaml` `T42`) with acceptance
    “one current-architecture doc”. Ratchet
    `scripts/docratchet/t47_install_doc_test.go:42-61` does not mention
    the package map. `git grep Package map` over `scripts/docratchet`
    → none.
  - 586 commits in the last 30 days (at initial HEAD). The page’s spine
    is still edited (T509 `3ea88f2d` 2026-08-21) — honesty of *some*
    paragraphs, not completeness of the cut.
- **Mechanism:** T42 prevents a few known lies and does not prevent
  omission. A new contributor (or T516 sensor) reading the map will not
  find capacity, RSI, audit, handover, supervise, envelope, planusage,
  or the agent/thread dual-write. Omission is how a second
  implementation of a mapped concern can land next to an unmapped one.
- **Blast radius:** every architectural decision, T516 sensors, onboarding.
- **Counterevidence checked:** the page correctly describes loopback,
  chatlog, claudia/pigeon, cost accounting modes, voice de-emphasis.
  Glossary still matches. This is not “the doc is fiction”; it is “the
  map stopped scaling”. T516 packet `:81` already says the page can be
  honest about a mess.
- **Smallest coherent remediation:** expand the package map to every
  `internal/*` package with a one-line role (generated or hand-maintained
  with a ratchet). Restate lifecycle as: fleet agents are the addressable
  participants; butler threads dual-write into that registry; `jwork` is
  the ephemeral Task path. Keep T41’s “legacy session tools are gone”.
- **Verification:** docratchet: set(internal dirs) − set(map rows) =
  allowed-omission list (cmd helpers, testdata). Lifecycle sentence
  must mention both `jevons_agent_*` and `jevons_thread_*` or explicitly
  mark one residual.
- **Ratchet candidate:** extend T42’s docratchet; do not rely on prose
  review.

### ENT-004: CI exercises Go, not the declared product net

- **Priority:** P1
- **Dimensions:** Correctness / verification; Build / release / operations
- **Status:** observed fact
- **Evidence:**
  - Declared net `Makefile:348-349`: `test: test-go test-web test-ui test-journey`
    (🎯T492).
  - CI `.github/workflows/ci.yml:23-38`: `go build` / `go vet` /
    `go test` with sqlite tags. No `node web/scripts/*_test.js`, no
    Playwright, no journey-suite.
  - Release `.github/workflows/release.yml` test job is the same Go-only
    shape (checkout@v6 vs CI’s checkout@v4 — extra drift).
  - `make bullseye` (`Makefile:380-386`) is build + `go test` + vet +
    clean tree — also no web/UI.
  - `scripts/docratchet` *does* ride `go test ./...`, so T42/T360/T398
    phrase ratchets run in CI. Node hermetics do not.
- **Mechanism:** AGENTS.md and T398 exist because web green on a dirty
  shared clone is not green on master. CI is the one place a clean tree
  is guaranteed, and it never runs `make test-web`. A cockpit regression
  of the T504 class can merge if it only fails Node/Playwright.
- **Blast radius:** every `web/scripts` and `web/index.html` change;
  owner-visible chat. Journeys (Universe B) arguably cannot run in
  GitHub without a provider — that part may be accepted risk. Node
  hermetics have no such excuse.
- **Counterevidence checked:** journeys need Grok and a throwaway daemon
  (AGENTS.md Universe B); missing provider is OUTAGE, not skip. Playwright
  needs the browser-loop-test install. Go tests include butler e2e, cost
  drill, mcpserver hermetics — the daemon is not untested. Local `make test`
  is the intended full net; CI is the remote subset.
- **Smallest coherent remediation:** add a CI job (or matrix step) that
  runs `make test-web` on a clean checkout (T398 already cares). Optionally
  `make test-ui` if Playwright is cacheable. Leave journeys as an owner-host
  gate with OUTAGE ≠ green. Document the split in architecture-current.
- **Verification:** CI workflow contains a step whose command is
  `make test-web` (or the same node list). A revert of that step fails
  the hygiene/docratchet once declared.
- **Ratchet candidate:** `hygiene.yaml` `ci_job` / `make_target` once
  hygiene is onboarded; until then a docratchet on `.github/workflows/ci.yml`.

### ENT-005: Two durable identity stores and two MCP lifecycles for one participant

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Change amplification
- **Status:** observed fact (dual-write is declared); inference (drift is likely)
- **Evidence:**
  - Persistence table `docs/architecture-current.md:240-241`:
    `~/.jevons/agents.json` and `~/.jevons/threads.json`.
  - `internal/thread/thread.go:46-47`: “Dual-write into the agent
    registry on Launch keeps one logical participant model (🎯T114)”.
  - `internal/butler/butler.go:62-71`, `:104-105`: `Participants` is the
    secondary lookup for agents that exist only in the fleet registry.
  - `internal/butler/push.go:30-31`: PushEvent uses Deliver so
    agent-only names succeed instead of “no thread”.
  - Two MCP registration sites: `SetRegistry` / `jevons_agent_*`
    (`internal/mcpserver/agents.go`) and `SetButler` /
    `jevons_thread_*` (`internal/mcpserver/threads.go:19-92`).
- **Mechanism:** a spawn through `jevons_agent_start` and a spawn through
  `jevons_thread_spawn` are supposed to converge on one participant.
  Dual-write can miss a field (purpose, parent, provider, role) on one
  path; T114, T111.2, T111.3 exist because it already did. The next
  attribute (T511 `role`) has to be written in both stores and both
  tool schemas or it disagrees.
- **Blast radius:** fleet tree, kill/lineage, rehydrate, inspect, PO
  spawn. Operator state in `~/.jevons` (not audited here).
- **Counterevidence checked:** this is *declared* dual-write, not an
  accidental clone. One deliver path (ENT-001 counterevidence) addresses
  both. Tests in `internal/butler/push_test.go` cover agent-only names.
  Collapsing to one store is a re-cut (T516 class “re-architect”), not a
  cleanup.
- **Smallest coherent remediation:** pick agents.json as the durable
  identity (claudia already owns it); make threads.json a derived
  projection or a butler-only overlay (adopted/observe-only kind). Keep
  `jevons_thread_adopt` as the observe-only door. Mark `thread_spawn` as
  a wrapper over `agent_start` with purpose=aside defaults, or retire it
  after a caller grep.
- **Verification:** hermetic: spawn via each MCP door, `agent_list` and
  `thread_list` show one identity with equal parent/purpose/provider;
  mutating one attribute on one door updates the other. Fail if a field
  exists on only one struct.
- **Ratchet candidate:** test in `internal/mcpserver` comparing the two
  structs’ overlapping JSON keys after Launch.

### ENT-006: iOS still compiles Lua, sqlpipe, and a third UI generation

- **Priority:** P2
- **Dimensions:** Architecture topology; Redundancy / sources of truth; Change amplification
- **Status:** observed fact
- **Evidence:**
  - Declared thin client: `docs/architecture-current.md:34-36`.
  - `ios/project.yml:27-41` compiles `Vendor/lua-5.1.5/src`,
    `Vendor/sqlpipe/…`, `Vendor/sqldeep/dist/sqldeep.cpp`, and
    `../web` as resources.
  - `ios/Jevon/Views/ContentView.swift:14-29`: WebUIView if pairing
    artifact; else ServerView from `connection.mainView`; else
    purpose-built `fallbackView` (ConnectView / ChatView path).
  - `ios/Jevon/Models/Connection.swift:419-420`, `:455-466`:
    `.scripts` → `LuaRuntime()`. Comment on `LuaRuntime.swift:25-26`
    still claims to mirror `internal/ui/lua.go` — that Go file is gone
    (`git ls-files '*lua*'` is docs + iOS vendor only).
  - `STABILITY.md:191-197`: “Lua view script runtime (🎯T9) is
    partially implemented — server-side rendering works; client-side
    Lua on iOS is not yet wired”; sqlpipe T10 incomplete. T10 in
    bullseye is `set_aside` (iPad-primary park).
  - `docs/lua-membrane.md:1` “Partly superseded”; `docs/todo.md:6-9`
    still asks to cache Lua and Lua-ify SwiftUI modifiers.
  - `ios/Jevon/JevonsApp.swift:9,20-35` still constructs and wires
    `VoiceManager` into the scene (voice is de-emphasized at
    architecture-current `:268-275`, but the iOS path is live code).
- **Mechanism:** three UI generations in one binary. A pairing-path user
  hits WKWebView (product). A no-artifact or server-driven path can still
  execute Lua and native Swift views whose server counterpart was
  removed. Compile cost and symbol surface stay large; STABILITY
  disagrees with Connection.swift about whether Lua is wired.
- **Blast radius:** iOS app size, Xcode project, anyone implementing
  T10/T9, connect/pair flows.
- **Counterevidence checked:** production path with a pairing artifact
  *is* WebUIView (`ContentView.swift:16-21`). ConnectView is a real
  fallback for unpaired devices, not automatically dead. Vendor trees
  were excluded from loc/clone analysis. T10 park is an explicit owner
  decision — deleting sqlpipe from the iOS target is that decision’s
  follow-through, not a new product call.
- **Smallest coherent remediation:** if T10 stays set_aside, drop
  sqlpipe/sqldeep/Lua from `project.yml` and delete or quarantine
  `LuaRuntime.swift` / `ServerView.swift` / ChatView/SessionList behind
  the unpaired ConnectView-only path. Align STABILITY with the remaining
  code. Promote leftover `docs/todo.md` Lua items into bullseye or
  delete them (ENT-009).
- **Verification:** `xcodegen` + build with Lua sources removed fails
  only if a still-linked Swift file references `lua_*`; a docratchet
  that `project.yml` does not list `Vendor/lua` unless T9 is reopened.
- **Ratchet candidate:** file rule / docratchet on `ios/project.yml`
  sources; STABILITY Lua sentence must match.

### ENT-007: Fleet doctrine is copied across four instruction corpora

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Change amplification
- **Status:** observed fact
- **Evidence:**
  - `internal/config/persona.md` (850 lines HEAD) — overseer identity,
    T98, fleet slices.
  - `agents-guide.md` (894) mirrored to `internal/cli/help_agent.md`
    via `Makefile:7-10` (T360, deliberate, ratcheted).
  - `internal/mcpserver/fleet_brief.go:8-11` `FleetStandingBrief`
    (269 lines HEAD) prepended on first `jevons_agent_send` — T176,
    T104, T31, T427, T188, …
  - Repo `AGENTS.md` (~487) restates the same fleet rules for coding
    agents working *in* this clone.
  - T511 (roles as the type an agent is spawned as) is in-flight in the
    dirty tree (`internal/config/roles/` untracked at start); HEAD
    `agents.go` start tool already mentions role in the working copy.
- **Mechanism:** a standing rule (oracle-first, local delivery, status
  language, PO never implements) has to be edited in N places or it
  disagrees. T176/T31/T125 already list persona + agents-guide + fleet
  brief as the three doors. AGENTS.md is a fourth. Roles files would be
  a fifth if they restate rather than replace.
- **Blast radius:** every fleet spawn; overseer behaviour; workers that
  only see the brief.
- **Counterevidence checked:** help_agent.md is a *generated mirror*
  (good). Persona is templated for the overseer; fleet_brief is for
  children — some duplication is audience-split, not accidental.
  T511’s stated intent is to *stop* if-you-are-X prose in the shared
  brief. That direction is the remediation.
- **Smallest coherent remediation:** one doctrine file per rule, included
  by persona / brief / AGENTS.md (already the agents-guide ↔ help_agent
  pattern). Role files own role-specific behaviour only. Docratchet
  selected phrases (T176, T31, T125) already exists in places — extend
  rather than adding a fifth copy.
- **Verification:** existing docratchet phrase tests; add a test that
  fleet_brief and persona both contain the T31/T176 markers *or* that
  one includes the other by generation.
- **Ratchet candidate:** extend T360-style mirror or a shared snippet;
  do not add a new corpus.

### ENT-008: hygiene.yaml is absent while T516 treats it as a live floor

- **Priority:** P2
- **Dimensions:** Documentation / governance; Correctness / verification
- **Status:** closed (2026-08-23, 🎯T538.8)
- **Closed by:** owner-onboarded `hygiene.yaml` from reality. Floors
  equal the held tier of each dimension; gaps sit in the aspires band
  as `planned`/`skipped`. `hygiene_check.py` PASS (correctness T1,
  perf T0, remaining dimensions T2). T516’s “hygiene.yaml declared
  steady-state floors” is now true. ENT-004 and ENT-010 stay open —
  they are declared gaps in the file, not closed by onboarding it.
- **Original evidence (2026-08-22 snapshot):**
  - `ls hygiene.yaml` → absent; `git ls-files hygiene.yaml` empty.
  - `/Users/marcelo/.claude/skills/hygiene/hygiene_check.py` exit 1:
    `FileNotFoundError: …/jevons/hygiene.yaml`.
  - `docs/design/architecture-entropy-watch.md:62,82` and
    `bullseye.yaml` T516 (`HEAD:bullseye.yaml:12421`, status
    `identified`) describe “hygiene.yaml declared steady-state floors”.
- **Mechanism:** a design packet and a needs-owner target encode a
  control that was never initialized. T516 sensors that “read hygiene
  drift” would no-op or crash the same way the validator did. This
  audit cannot report held tiers or floors because they were never
  declared.
- **Blast radius:** T516 implementation; fleet hygiene aggregation;
  this report’s hygiene section.
- **Counterevidence checked:** AGENTS.md / Makefile already *behave*
  like a high-hygiene repo (tests, LICENSE Apache-2.0, CI, release).
  Absence is undeclared posture, not missing tests. Skill forbids
  initializing hygiene.yaml during an entropy audit.
- **Smallest coherent remediation:** owner-onboard `hygiene.yaml` from
  reality (CI Go test, `make test-web` as planned, govulncheck skipped
  with reason, etc.) *or* strike the hygiene.yaml sentences from T516
  until onboarded. Do not pretend floors exist.
- **Verification:** `hygiene_check.py` exit 0 against the new file, or
  T516 prose no longer claims the file exists.
- **Ratchet candidate:** the hygiene validator itself, once a file
  exists.

### ENT-009: `docs/todo.md` is a live competing work ledger

- **Priority:** P3
- **Dimensions:** Documentation / governance
- **Status:** observed fact
- **Evidence:**
  - Tracked file `docs/todo.md:1-9` — Lua caching, Claude session
    archaeology (mnemo already owns this), Lua-controllable SwiftUI
    props. Several items contradict architecture-current and T10
    set_aside.
  - Global agent instructions ban TODO files in favour of bullseye
    (`~/.claude/CLAUDE.md` task-tracking rule). Repo AGENTS.md uses
    bullseye as the canonical followable-work record.
- **Mechanism:** a contributor (or agent) reading `docs/todo.md` will
  implement Lua membrane work that the architecture page calls
  superseded.
- **Blast radius:** iOS/Lua (ENT-006); session-memory (already mnemo).
- **Counterevidence checked:** some lines are struck through as Done.
  File is historical. T42 asked superseded docs to carry banners;
  `docs/todo.md` has none.
- **Smallest coherent remediation:** banner + “do not execute; see
  bullseye”, or delete after promoting any still-live line (OCR fallback
  is the only one that is not obviously superseded).
- **Verification:** docratchet that `docs/todo.md` does not exist, or
  starts with a superseded banner naming bullseye.
- **Ratchet candidate:** file-absent rule, same family as T42 banners.

### ENT-010: No automated vulnerability or secret scan in CI

- **Priority:** P3
- **Dimensions:** Security / dependencies
- **Status:** observed fact
- **Evidence:**
  - `git grep` over workflows/Makefile/docs for `govulncheck`,
    `golangci`, `dependabot`, `codeql`, `gosec` → no matches.
  - `.github/` contains only `ci.yml` and `release.yml`.
  - architecture-current `:262-266` and STABILITY honestly say
    permissions-bypassed workers and mTLS off by default.
- **Mechanism:** dependency CVEs and accidental secret commits are
  undetected except by human review. Pre-commit does not secret-scan.
  Single-operator loopback reduces *network* exposure; it does not
  reduce supply-chain or committed-secret risk (`go.mod` pulls AWS SDK
  indirectly via claudia/bedrock).
- **Blast radius:** supply chain; any future bind-wider than loopback.
- **Counterevidence checked:** declared threat model is
  single-trusted-operator. Hard rule “never commit secrets” is process,
  not a scanner. Adding scanners is hygiene-tier work, not an
  architecture re-cut. Do not treat this as a P0 while bind stays
  loopback and workers are owner-trusted.
- **Smallest coherent remediation:** `govulncheck ./...` in CI
  (warning); optional `gitleaks`/`secret-scan` on the same job. Record
  as `planned` in hygiene.yaml when onboarded.
- **Verification:** CI job exists; a known vulnerable module in a test
  overlay fails it (or a skipped reason is declared).
- **Ratchet candidate:** hygiene `scanner:` evidence.

### ENT-011: Dormant voice stack remains on the composition path

- **Priority:** P3
- **Dimensions:** Change amplification; Architecture topology
- **Status:** observed fact
- **Evidence:**
  - architecture-current `:268-275` — voice de-emphasized; dormant
    machinery in `/ws/voice`, `internal/server/voice*.go`,
    `VoiceManager.swift`.
  - HEAD loc: `internal/server/voice.go` 805, `voice_fsm.go` 477.
  - `ios/Jevon/JevonsApp.swift:9,20-35` still injects VoiceManager into
    the environment and routes utterances into `connection.send`.
- **Mechanism:** chat/server changes can still collide with voice
  types and `/ws/voice`. iOS still pays the wiring. Not a current
  product path (Wispr Flow dictation is the declared input).
- **Blast radius:** `internal/server` hub (ENT-001); iOS scene.
- **Counterevidence checked:** explicit T37 no-go; keeping the code is
  an accepted dormant residual, not an accident. Deleting it is owner
  taste (T22 may resume).
- **Smallest coherent remediation:** quarantine under a build tag or
  `voice/` package that `server` optionally registers, so chat.go does
  not keep paying; or delete when T22 is parked for good.
- **Verification:** `go test ./internal/server` green with voice files
  moved; grep `/ws/voice` confined to that package.
- **Ratchet candidate:** none until T37/T22 is re-opened. Owner residue.

## Redundancy and competing-source-of-truth inventory

| Concern | Instances | Drift risk | Disposition |
|---|---|---|---|
| Agent identity | `agents.json` (claudia) + `threads.json` (butler) | High — dual-write (T114) | ENT-005; declared |
| Agent lifecycle MCP | `jevons_agent_*` + `jevons_thread_*` | High | ENT-002, ENT-003, ENT-005 |
| MCP catalogue | STABILITY table vs `mcp.NewTool` | High — already drifted | ENT-002 |
| Architecture cut | architecture-current map vs `internal/*` | High — already omitted | ENT-003 |
| Chat events | `chat_wire.go` + `chat_events.js` | Medium — protocol copy, tests exist | **Keep** (deliberate two-sided wire) |
| Agent guide embed | `agents-guide.md` → `help_agent.md` | Low — Makefile + T360 ratchet | **Keep** |
| Fleet doctrine | persona.md, agents-guide, fleet_brief, AGENTS.md | High | ENT-007 |
| Spend | `internal/cost` vs `planusage` vs `harnessusage` | Low — different jobs, documented | **Keep** |
| RSI | `jevons_rsi_coach_*` vs residual `jevons_rsi_cycle` | Medium — documented residual | Keep residual labelled; list in STABILITY |
| Staff loops | RSI coach, T357 audit, T356 research, sentinel, staff_ops | Medium — capacity-gated, separate jobs | Keep; T516 would be another sibling, not a merge |
| iOS UI | WebUIView, ServerView+Lua, native Connect/Chat | High | ENT-006 |
| Work ledger | bullseye.yaml vs `docs/todo.md` | Medium | ENT-009 |
| Hygiene floors | T516 prose vs missing `hygiene.yaml` | High | **Closed** 🎯T538.8 (file onboarded; validator PASS) |
| Display fold | `foldDisplayEvent` vs `applyLiveDisplayFrame` | Was high (T504) | **Closed** on achieved T504; T516 wants a sensor so it cannot return |
| Conversation widget | main vs inspect | Was high (T309/T372) | Largely closed; `429e106b` during this audit deleted `renderAgentInspect` dump |

## Healthy structure worth retaining

- **No `internal/*` import cycles** (import-graph command). Leaves stay
  leaves.
- **`deliverByName` as the fleet send chokepoint**
  (`internal/mcpserver/deliver_by_name.go:15-24`) and **`butler.Fleet`
  as the process seam** (`internal/butler/butler.go:29-51`). Do not
  re-split notify/overseer wires.
- **Chat-wire two-sided copy with Node hermetics** (`chat_wire.go` /
  `chat_events.js` / `make test-web`). Protocol duplication here is
  cheaper than a shared language.
- **Cost L1/L2/L3** plus a *separate* observational harness-usage plane
  and a *separate* plan-usage consumer. Do not merge them: they answer
  different questions (clamp vs “what did humans spend” vs “what is
  left on the subscription”).
- **T360 embed mirror**, **T376/T377/T432 shared-clone guards**,
  **T386/T396 `bin/gate`**, **T405/T434 watchdog**, **T398 clean-checkout
  web ratchet** — these are entropy controls that already paid for
  themselves (incidents in their comments).
- **Supersession banners** on `docs/architecture.md:1` and
  `docs/lua-membrane.md:1` (T42). Keep that pattern; apply it to
  `docs/todo.md`.
- **Universe A vs B** port isolation for journeys (AGENTS.md; journey
  suite refuses `:13705`).
- **Honest security write-up** (architecture-current `:252-266`) —
  do not replace it with a scanner badge.

## Hygiene posture

**Closed 2026-08-23 (🎯T538.8).** `hygiene.yaml` is present. Validator:

```
$ /Users/marcelo/.claude/skills/hygiene/hygiene_check.py
hygiene: jevons   aspires tier 3
  dimension     held  floor
  correctness   T1    T1   ✓
  security      T2    T2   ✓
  quality       T2    T2   ✓
  deps          T2    T2   ✓
  release       T2    T2   ✓
  governance    T2    T2   ✓
  build         T2    T2   ✓
  docs          T2    T2   ✓
  perf          T0    T0   ✓
  vcs           T2    T2   ✓
  agent         T2    T2   ✓
PASS
```

Original 2026-08-22 snapshot (finding, not current state):

**Hygiene posture not declared.** `hygiene.yaml` is absent.

Validator invoke (mandatory even though the file is missing):

```
$ /Users/marcelo/.claude/skills/hygiene/hygiene_check.py
FileNotFoundError: [Errno 2] No such file or directory:
  '/Users/marcelo/work/github.com/marcelocantos/jevons/hygiene.yaml'
exit=1
```

No per-dimension held tiers or floors can be validated. T516’s
references to hygiene floors are ENT-008, not a passed check.

Overlap with entropy findings: ENT-004 (CI vs `make test`) and ENT-010
(scanners) are exactly the items a later `hygiene.yaml` would declare
as `enforced` / `planned` / `skipped`. Do not ratchet hygiene.yaml in
this audit.

Entropy findings suitable for future hygiene enforcement: ENT-001
fan-out/loc floors, ENT-002 MCP catalogue, ENT-003 package map,
ENT-004 `make test-web` in CI, ENT-010 `govulncheck`.

## Oracle coverage and residue

| Property | Decided by | Notes |
|---|---|---|
| Go hermetic correctness | shipped `go test ./...` (CI + `make test-go`) | Not re-run here (dirty clone) |
| Web hermetic correctness | `make test-web` (local); **not CI** | ENT-004 |
| Playwright perceptual UI | `make test-ui` (local); **not CI** | ENT-004 |
| Owner-visible journeys | `make test-journey` Universe B | Not run; needs provider |
| Architecture-current selected truths | docratchet T47/T156/T509 | Phrase-level; misses map completeness (ENT-003) |
| Clean-checkout web tests | docratchet T398 | Exists; CI still does not run test-web |
| Embed inputs tracked | docratchet T360 | Healthy |
| Gate status honesty | `bin/gate` + docratchet T386 | Healthy |
| Deliver-by-name uniqueness | hermetics in `deliver_by_name_test.go` | Healthy seam |
| Dual agent/thread identity | partial butler tests | ENT-005 gap |
| MCP catalogue completeness | **nothing** | ENT-002 |
| Package import fan-out | **nothing standing** | ENT-001; T516 would be the sensor |
| Dependency CVEs | **nothing** | ENT-010 |
| iOS Lua still linked | **nothing** | ENT-006 |
| Hygiene floors | **undeclared** at audit; closed 🎯T538.8 | ENT-008 |
| Runtime agents.json vs threads.json | **not inspected** | operator state |

**Owner residue (intent / taste only)**

- Accept or park 🎯T516 (sibling architecture coach vs T357 lens vs
  park). Packet:
  `docs/design/architecture-entropy-watch.md`.
- Is `jevons_thread_*` product or compatibility? (ENT-005)
- Delete iOS Lua/sqlpipe while T10 is set_aside? (ENT-006)
- Is CI *allowed* to be Go-only if local `make test` stays the product
  net? (ENT-004 — Node hermetics still look like an accidental gap)
- Keep dormant voice in-tree for T22? (ENT-011)

Mechanical work (catalogue ratchet, package map, test-web in CI, TODO
banner) is **not** returned as owner residue.

## Remediation sequence

1. **Oracle seam, no behaviour change**
   - Docratchet: STABILITY MCP table ↔ `mcp.NewTool` (ENT-002).
   - Docratchet: package map ↔ `internal/*` (ENT-003).
   - CI step: `make test-web` on a clean checkout (ENT-004).
   - Banner or delete `docs/todo.md` (ENT-009).
   - Either onboard `hygiene.yaml` from this reality or strike it from
     T516 (ENT-008). Owner call. **Done 🎯T538.8:** onboarded.
2. **Converge competing truths**
   - Restate architecture-current lifecycle as agents + dual-write
     threads + jwork (ENT-003/005).
   - Decide thread MCP family (wrapper vs residual).
   - Align STABILITY Lua/sqlpipe sentences with `project.yml`
     (ENT-006) after the iOS cut decision.
3. **Remove residue only after callers are proven migrated**
   - Quarantine or drop iOS Lua/sqlpipe if T10 stays set_aside
     (ENT-006).
   - Extract remaining `index.html` inline script by concern; ratchet
     inline-line count downward (ENT-001).
   - Stop adding imports/tools to `mcpserver` without a package-map
     row (ENT-001/003).
4. **Ratchet** the accepted properties in CI (and hygiene.yaml if
   onboarded). Fan-out/loc floors are bidirectional — shrinking is a
   deliberate commit too.
5. **Re-run this audit** on the same definitions (tool list = `NewTool`
   names; package map = `internal/*` dirs; CI commands; hygiene file
   present or still undeclared). Compare to snapshot
   `cb06b532b6cc36ffb1f8f1568cb539c22c0c3416` / this document.

Do not implement T516 until the owner accepts or parks it in-band.
The sensors in the packet (dual implementation, hub growth, map drift)
are exactly ENT-001–ENT-003; a first cleanup example is already on
the tree (finish `index.html` extraction; stop STABILITY drift).
