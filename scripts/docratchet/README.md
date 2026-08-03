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
| T191 restart-daily | `scripts/restart-daily-jevonsd.sh` exists, executable, `/bin/bash -n`, `--help`/`--dry-run`, nohup/setsid detach markers; persona/AGENTS/agents-guide carry T188+T191 doctrine. Not a live daily bounce. |
| T194 daily-path achieve | persona/AGENTS/agents-guide/fleet_brief carry doctrine: daemon/API not achieved on hermetics alone; restart-daily + live probe; `HasDailyPathEvidence`. Pure classifier tests live in `internal/mcpserver`. Not a live daily bounce. |
| T197 worker names | Doctrine surfaces + `agents.go` spawn tool keep hierarchical worker-name examples with **literal dots** (`jv-t27.2-config` not `jv-t272-config`); flat residual `jv-t159-seal`. Spawn path hermetic: `TestEnsureAgentPreservesLiteralDotsInName`. |
| Daily port | Lives in `scripts/journey-suite/portguard` — real function unit test, not a doc grep. |

```bash
go test ./scripts/docratchet/ ./scripts/journey-suite/portguard/ -count=1
```
