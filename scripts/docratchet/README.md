# Doc ratchet (not user journeys)

Hermetic checks that **standing product phrases** and inventory IDs still
exist in committed docs/source.

These tests are **not** user journeys (🎯T107). They do not start
`jevonsd`, talk to Grok, or interact with an agent.

## What they actually assert

| Check | Behaviour under test |
|---|---|
| Doc markers | That `AGENTS.md` / journey README still contain fixed substrings (prose ratchet). Not `os.ReadFile` itself — the stdlib is assumed. If the doctrine text is deleted, CI fails. |
| Journey inventory | That J1–J10 IDs appear in README **and** as `s.run("J…")` registrations in suite source. Drift between docs and registration is caught. |
| T47 install path | That root `README.md` still names brew install, config, pair residual, adopt, and direct; architecture-current no longer claims pre-T6 all-interfaces default. Not a live second-user drill. |
| T156 relay self-host | That README names URL+token auth (`PIGEON_TOKEN` / `TERN_TOKEN`), self-host-only scope, copy-paste `go build -o pigeon`, and forbids author messaging for tokens / free-tier Fly claims. Not a live deploy. |
| T191 restart-daily | `scripts/restart-daily-jevonsd.sh` exists, executable, `/bin/bash -n`, `--help`/`--dry-run`, nohup/setsid detach markers; persona/AGENTS/agents-guide carry T188+T191 doctrine. Not a live daily bounce. |
| T194 daily-path achieve | persona/AGENTS/agents-guide/fleet_brief carry doctrine: daemon/API not achieved on hermetics alone; restart-daily + live probe; `HasDailyPathEvidence`. Pure classifier tests live in `internal/mcpserver`. Not a live daily bounce. |
| T509 typed envelopes | persona/AGENTS/agents-guide/fleet_brief/worker role point at `internal/envelope`; `jevons: kind` examples match `AllKinds()`; GREEN/SUSPECT/in-progress/class-3 live in that package. Not a live fleet send. |
| T493.1 visual cockpit prose | persona/AGENTS/agents-guide/fleet_brief carry doctrine: after #messages visual work, a four-part prose look (ink / empty / Latest / normal transcript yes-or-no); `visibleInScroller` and screenshot-tool captions are not the verdict; automatic no; false-green journey same-turn oracle fix; `HasVisualProseVerdict` / `LooksLikeMissingVisualVerdict`. Pure classifier tests live in `internal/mcpserver`. Not a live screenshot. |
| T262.2 frontier map | `docs/design/frontier-as-ready-set.md` keeps the worker-per-leaf map (T155/T193/T198/T222 + T254.1–T254.6 residuals), the policy-on-set classification (capacity / shared-file ownership / design-park filters / churn ≠ global queue), a disposition per residual, and the no-unpark / no-T262.4 non-claims. Not a live spawn. |
| T262.3 ownership table | Same packet keeps the cross-product table (each Beads-ish surface → Bullseye / Jevons / neither), the bullseye-side candidates (next-ticket UX language, assign/unassign as exclusion ≠ queue head, ranking as optional policy not permission), the jevons-side candidates (spawn caps, engagement UI, factory continuity without Beads mail), and the explicit out-of-scope list (no Beads dual-write, no T254 unpark, no graph-math change). Not an implementation of any candidate. |
| T197 worker names | Doctrine surfaces + `agents.go` spawn tool keep hierarchical worker-name examples with **literal dots** (`jv-t27.2-config` not `jv-t272-config`); flat residual `jv-t159-seal`. Spawn path hermetic: `TestEnsureAgentPreservesLiteralDotsInName`. |
| T360 clean checkout | A detached `git worktree` of **HEAD** in a temp dir runs `go build ./...` green with no prior `make`. Plus the fast general form: no `//go:embed` pattern resolves to a gitignored path, and `internal/cli/help_agent.md` still matches `agents-guide.md`. Not a doc grep — a real build of the committed tree. |
| T398 clean-checkout web | The web half of the same shape: a detached worktree of **HEAD** runs `make test-web` green. Many workers share one working copy, so the suite run there reads everyone's WIP and can stay green while a fresh clone gets red — which is how master stayed red from 8297ae6 until 🎯T388's gate noticed. Not a doc grep — a real run of the committed tree. |
| Daily port | Lives in `scripts/journey-suite/portguard` — real function unit test, not a doc grep. |

```bash
go test ./scripts/docratchet/ ./scripts/journey-suite/portguard/ -count=1
```
