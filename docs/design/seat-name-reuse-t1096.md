# Seat name reuse after kill (🎯T1096)

## Problem

Worker names are deterministic (`jv-t<n>-auto`, PO remints, journey fixtures). A
`jevons_agent_kill` or finished-work reap **removes** the registry row. The OMP
sidecar can still hold an in-memory seat under that name (or on disk under the
old session). A later `jevons_agent_start` with a **new** session and/or
provider then failed with:

`omp: loaded seat differs from registered provider or purpose: load seat identity mismatch`

That blocked frontier re-mints (🎯T155) and journey reuse (J45 on 2026-10-20).

## Intended behaviour (Claudia v0.62.0+)

Three cases, tested in Claudia `t1096_seat_reuse_test.go` and pinned on the Jevons
path in `internal/mcpserver/t1096_remint_test.go`:

| Case | Trigger | Outcome |
|------|---------|---------|
| **(a) Clean removal** | `Registry.Remove` / `jevons_agent_kill` (deregister) | The running OMP seat is **dropped** (`OpDrop`) as the handle closes. The name is free; a later start may use another provider and a fresh `session_id`. |
| **(b) Stale in-memory seat** | Fresh `load` (no `require_resume`) while the sidecar still holds the name under an **older** session | Claudia **drops** the stale loaded seat and **retries** the load. Prior conversation remains on disk under the old session path only. |
| **(c) Resume required** | `require_resume` with a session that does not match what is loaded | **Refused** with `ErrSeatIdentityMismatch` and owner-readable text: names the seat, the requested session, that another session is loaded, and to kill or use another name — not the opaque “provider or purpose” line. |

Jevons obligations:

- Kills and reaps go through `fleetlog.Account.Remove` → `reg.Remove` (not
  `RemoveKeepingBackend`), so case (a) always runs on the product path.
- Starts and frontier spawns mint a **new** `session_id` and do not set
  `RequireResume` for a name that was just removed (fresh auto remint).

## Residual

Hermetic Jevons tests use `SetDirect` + `StartStub`; they prove registry removal
and launch config, not the live sidecar drop/retry wire. Claudia owns OMP case
(b)–(c) regression tests including the real sidecar (`daemon/t1096_seat_reuse_test.go`).
