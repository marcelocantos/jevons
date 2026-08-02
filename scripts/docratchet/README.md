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
| Daily port | Lives in `scripts/journey-suite/portguard` — real function unit test, not a doc grep. |

```bash
go test ./scripts/docratchet/ ./scripts/journey-suite/portguard/ -count=1
```
