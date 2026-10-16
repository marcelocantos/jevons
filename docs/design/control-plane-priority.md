# Control-plane priority (🎯T779)

Status: **implemented** (host LaunchAgent + check oracle). Residuals named below.

## The problem

Measured 2026-09-21 (and reconfirmed 2026-10-14): Homebrew `supervisord`
and every child it owns — `jevonsd`, the claudia broker, `bullseye`,
`mnemo`, and *their* children (`git`, `lsof`, ACP clients) — run at
Mach scheduling priority **PRI 20**. Fleet agent shells and interactive
terminals run at **PRI 31**. Same `nice` (0); the gap is launchd's
resource class, not POSIX nice.

Under fleet load the control plane is starved. The original incident:
bullseye's `git` child received 0.83s CPU in 29s wall (~30× starved) and
was killed. MCP timeouts, gate hangs, and "daemon not responding" during
fan-out are the same shape.

launchd A/B on this host: adding `ProcessType=Interactive` to the
supervisor LaunchAgent restores PRI 31 for supervisord and its children.

## Owner decision (recorded)

**Raise the control plane**, do not lower the fleet by default.

- Set `ProcessType=Interactive` on `homebrew.mxcl.supervisor` (the
  LaunchAgent that owns supervisord and therefore every supervised
  control-plane daemon).
- Optional later: lower fleet work via an owned seat-spawn path if
  Interactive on the whole supervisor tree proves too broad. Not the
  first move — fleet work is the product; the control plane must not
  lose to it.

`ProcessType=Interactive` is the documented launchd class for jobs an
app's responsiveness depends on (see `man launchd.plist`). The cockpit,
MCP tool plane, and broker are exactly that for this machine.

Claudia's own LaunchAgent template already carries
`ProcessType=Interactive` (`claudia install`), but the *running* broker
on this host is a supervisord child, so the supervisor job's class is
what counts.

## Shape of the fix

| Piece | Role |
|---|---|
| `supervisor/ensure-interactive-priority.sh` | Idempotently ensures `ProcessType=Interactive` on the supervisor LaunchAgent plist. Default: write only. `--apply` bootstraps/kickstarts so the class takes effect (restarts every supervised service). |
| `supervisor/check-control-plane-priority.sh` | Live oracle: samples PRI for supervisord children vs a fleet-shell baseline; exits non-zero when control plane is below fleet. |
| `internal/hostprio` | Pure classifier over `ps`-shaped rows (hermetic). |
| `supervisor/install.sh` | Calls ensure (write-only) so a fresh install does not leave the throttled class in place; bounce remains explicit. |

`brew services reinstall supervisor` can rewrite the LaunchAgent from
the formula and drop the key. Re-run ensure (or `make
supervisor-interactive`) after a brew bump; the check oracle flags
regression.

## Activation

Writing the plist is not enough — launchd applies `ProcessType` at job
start. `--apply` / kickstart restarts supervisord and every child. That
is disruptive (MCP flap, brief cockpit gap) and is therefore opt-in, not
a silent side effect of `install.sh`.

## Residuals

- **Per-program class.** All supervisord children inherit Interactive
  once the parent does, including ambient helpers that are not
  control-plane critical. Fine for a single-user development host;
  multi-tenant would want a split.
- **Fleet lowering.** Not implemented. If Interactive supervisor proves
  too greedy under heavy GUI load, file a follow-up to renice or
  QoS-tag seat shells downward.
- **Non-supervisor control paths.** A leftover KeepAlive `jevonsd` or
  `brew services` claudia outside supervisord is out of scope; those
  paths are already retired by 🎯T594 / install.sh takeover.
