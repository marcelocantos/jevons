# 🎯T1103 — the restart/sentinel pause contract

## The incident

2026-10-21 09:01 the owner approved re-enabling frontier auto-consumption.
`scripts/restart-jevonsd.sh` rebuilt and started jevonsd (served commit
`0a9597b4`), and the 2026-10-15 `autostart=false` lines in the supervisord
program config were restored to `true` as part of that same approval.

At 09:05:00 a separate host sentinel (`~/.local/bin/sen --serve`, a Codex
session rooted at `~/.local/share/sen`) acted on a standing order it was
still holding: a marker file `jevons-paused.txt` reading "Paused by owner
on 2026-10-15; do not start Jevons until explicit owner instruction", and
its own `MEMORY.md` ("keep it stopped until further notice"). It ran
`supervisorctl stop jevonsd`, rewrote `jevonsd.ini` back to
`autostart=false` / `autorestart=false` (backing up the live config as
`jevonsd.ini.sen-revival-20261021`), and reported the owner-authorised
restart to the owner as "Jevons has restarted unexpectedly under
Supervisor" — an incident, not the owner action it actually was.

Nothing on the Jevons side knew the sentinel held a pause. Nothing on the
sentinel side was told the owner had lifted it. The two processes shared
no contract, so the second one to act always wins, and here it reverted a
decision the owner had just made minutes earlier.

Resolved by hand on the same day: the sentinel's `MEMORY.md` pause section
was replaced with an unpause note (backed up as
`MEMORY.md.pre-unpause-20261021`), the marker file was renamed to a dated
record (`jevons-paused-20261015-lifted-20261021.txt`) rather than deleted,
the `.ini` was restored, and jevonsd was started directly (pid 73803).

Sibling of 🎯T1086 (the supervisord half of "who owns the port") and
🎯T405 (two supervisors with no shared contract — there, launchd vs.
supervisord; here, a sentinel's standing order vs. a fresh owner
instruction).

## The contract

There is exactly one pause marker, and it names which side writes it and
which side clears it:

```
~/.local/share/sen/jevons-paused.txt
```

(overridable for testing via `JEVONS_RESTART_PAUSE_MARKER`; see below).

**Who writes it.** The sentinel, and only the sentinel, when the owner
tells *it* to pause Jevons. The file's content is free text read verbatim
by whoever refuses on it — it should say who paused it, when, and what
lifts the pause.

**Who reads it.** `scripts/restart-jevonsd.sh` and
`supervisor/install.sh` (🎯T1103), before either one does anything that
would bring the development jevonsd up: rebuild-and-activate, or
supervisord render/start. Both refuse if the marker exists, printing its
contents and the override, and exit nonzero (`3`) without touching the
daemon, the port, or supervisord's config. Neither script deletes the
marker — lifting a pause is the sentinel's job, not the restart path's.

**Who clears it.** The sentinel, when it next observes (or is told) that
the owner lifted the pause — by renaming or removing the marker, the way
the 2026-10-21 incident was closed by hand. A clear should be a dated,
auditable move (rename to `jevons-paused-<set>-lifted-<cleared>.txt`), not
a silent delete, so a later reader can reconstruct the timeline the way
this document does.

**The override.** `JEVONS_RESTART_OVERRIDE_PAUSE=1` on either script's
invocation proceeds despite the marker. This is for the owner's explicit
word in the moment — a worker or the PO never sets it on their own
initiative, because the marker existing at all means a human has already
intervened and decided the standing state should be "stopped." Setting
the override without the owner's go-ahead is how this incident happens
again, forwards: a worker racing a sentinel that may still be about to
act on an older order.

## What the sentinel owes the restart path

This is the half the sentinel's own `MEMORY.md` / `RUNBOOK.md` should
carry, in the sentinel's own words, so the sentinel reads it where it
already looks:

- A `RUNNING` jevonsd with `autostart=true`, after a restart that is
  logged in `~/.jevons/restart-jevonsd.log`, is **an owner action to
  report**, not an incident to revert. The log line names who ran the
  restart and when; read it before concluding the daemon "restarted
  unexpectedly."
- Stopping jevonsd through Supervisor again requires a **fresh** owner
  instruction naming the date. A standing order from an earlier date does
  not re-arm itself against a later, contradicting owner action — the
  more recent instruction wins, and "more recent" is read from the
  instruction's own timestamp, not inferred from which process got there
  first.
- If the sentinel believes a pause is still standing and sees the daemon
  running anyway, the correct response is to ask the owner whether the
  pause still holds — writing the marker (if it does not already exist)
  or stopping the daemon on its own account are both race-prone compared
  to asking.

## Testing

`scripts/docratchet/t1103_pause_contract_test.go` plants a marker at a
`JEVONS_RESTART_PAUSE_MARKER` override path and drives
`restart-jevonsd.sh --dry-run` (which never touches the daemon by itself)
and `supervisor/install.sh` through it, asserting:

- with the marker present, both scripts refuse (`exit 3`), neither one
  prints a dry-run activation plan or renders a `.ini`, and the refusal
  quotes the marker's own text and names the override;
- `-h`/`--help` on the restart script is exempt from the pause (reading
  the manual is never blocked);
- with `JEVONS_RESTART_OVERRIDE_PAUSE=1` set alongside the same marker,
  both scripts proceed past the check.
