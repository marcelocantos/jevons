# T511 fleet standing brief — universal vs role buckets

Owner review is post-land; this table records implementer judgment for each
`##` section in `internal/mcpserver/fleet_brief.go` (44 sections, 2026-10-21).

| Section | Bucket | Role file(s) | Notes |
|---------|--------|--------------|-------|
| Cross-repository target references (T1056) | universal | — | |
| Never signal the daemon (T1067) | universal | — | |
| Status language: in progress vs live (T176) | universal | overseer, worker | Overseer gates others; workers cite in progress |
| Routine worker reports (T1053) | universal + overseer | overseer | Pinned in fleet_brief by docratchet T1122 |
| Fleet state narration (T692) | universal | all roles | Header no longer duplicates per-role T692 bullets |
| Fleet intent records who decided (T969) | universal | — | |
| Environments: development vs released (T572) | universal | — | |
| Delivery: local by default (T104) | universal | worker | Commit-when-done binds workers |
| Oracle-first completion (T31) | universal | overseer, worker | Overseer is independent gate |
| Cited SHA must stay reachable (T427) | universal | overseer, worker | |
| Run gates (T386 / T396) | universal | worker | |
| Owner-visible claims observed (T552) | universal | — | |
| Owner-visible means owner-relevant (T1122) | universal | — | Docratchet-pinned |
| Overseer owner chat declares purpose (T1140) | universal + overseer | overseer | Pinned in fleet_brief by docratchet |
| Only the owner speaks (T1129) | universal | worker | |
| Visual cockpit finish (T493.1) | universal | worker | |
| Cockpit UI path (T540) | universal | worker | |
| Greenfield oracle elicitation (T31.2) | universal | — | |
| A send is confirmed (T416) | universal | — | |
| Guard tests reach production (T419) | universal | — | |
| Fleet spawn (T78) | universal | product-owner | PO executes spawn |
| Worker names: literal dots (T197) | universal | — | |
| Multi-slice fan-out (T111.4) | role | boss | Boss role file carries fan-out duty |
| Frontier = ready set (T262.1) | universal | product-owner | PO consumes frontier |
| Unattended frontier auto-spawn (T155) | role | product-owner | Moved out of universal brief |
| File→spawn same turn (T193) | role | product-owner | Moved out of universal brief |
| PO proactive-until-empty (T325.1) | role | product-owner | Moved out of universal brief |
| Silent-decision ledger (T536.1) | universal | worker, auditor | |
| Waiting on the owner (T938) | universal | worker | |
| Actively waiting (T1024) | universal | worker | |
| A report that declares nothing (T766.4) | universal | — | |
| The done finish-report shape (T1083) | universal | worker | Worker role cites shape; not triple-copied in header |
| Reap vs park (T985) | universal | worker | |
| Fog-of-war scout (T536.3) | universal | worker | |
| PO never implements (T125) | role | product-owner | Removed from universal brief |
| Overseer never parents (T129) | role | overseer, product-owner | Removed from universal brief |
| Filing reflex (T130) | universal | product-owner | PO files; workers may prompt-file |
| Shared-workdir hygiene (T1113) | universal | worker | |
| Context remint (T561) | universal | — | |
| Switching provider or model (T195) | universal | — | |
| Plan dest ranks (T693) | universal | overseer | |
| Parent report daemon-delivered (T690) | universal | worker | |
| Concurrent live-render browser (T430) | universal | worker | |
| Report | role | worker | Worker finish obligations; PO/overseer stay registered |
| Landing never stashes (T1134) | universal | worker | concat const |
| Ledger merge conflicts (T1145) | universal | worker | concat const |

Authority: **role files are the single source for role-conditional doctrine**
(owner fork 1). The T425 identity header names the role and points at the winning
role file; `EnsureFleetBriefWithRole` injects that body under `[Jevons role doctrine]`.

**Phase 5 (overseer / 🎯T44):** **compose** — `internal/config/persona.md` stays the
owner-templated shell (CEO voice, tools, filing reflex, T1082 state-dir isolation);
`internal/roles/builtin/overseer.md` is authoritative for overseer-seat doctrine.
`config.Config.Persona()` appends the role body when `cmd/jevonsd` writes AGENTS.md /
Claude.md (no third embedded prompt in main.go).
