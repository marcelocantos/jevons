#!/usr/bin/env bash
# 🎯T191 / 🎯T553 — rebuild + activate the development jevonsd on :13705.
# Workers do not auto-invoke this (🎯T553.2). supervisord owns the
# development process (supervisor/jevonsd.ini, 🎯T1111); this script asks it
# for an upgrade exit and starts a detached daemon only where nothing
# supervises the port.
#
# Purpose: overseer/PO/worker bounce after daemon-path Build without asking
# the owner to restart by hand (🎯T188). Session death must not cancel the
# bounce, and since 🎯T405 that no longer depends on the caller: the script
# re-execs itself into its own session unconditionally (see SELF-DETACH).
#
# Steps:
#   0. re-exec into our own session (🎯T405 SELF-DETACH below)
#   1. make / rebuild bin/jevonsd with its embedded React bundle
#   2. decide whether a restart is needed at all (🎯T218 below)
#   3. brew services stop jevons (so Cellar KeepAlive cannot reclaim :13705)
#   4. SIGHUP listeners on the development port — 🎯T40 upgrade exit, so agent
#      turns in flight are NOT cancelled by the bounce (🎯T392.5)
#   5. start $REPO/bin/jevonsd with workdir set (detached: nohup/setsid)
#   6. wait until HTTP /health is ok and /api/frontier is not 404
#   7. exit 0 only when the new process is serving
#
# 🎯T218 — why this script often does nothing, on purpose.
#
# Many fleet workers land daemon-path changes concurrently and each calls
# this script. Two rules keep that from bouncing a healthy daemon for
# nothing, without ever letting a real change go stale (🎯T194 says a
# daemon change is not achieved until it actually serves):
#
#   ALREADY-ACTIVATED — if the freshly built binary hashes equal to the one
#     the running daemon was started from, and that daemon is healthy, there
#     is nothing to activate. Exit 0 without touching the port.
#
#   ONE-AT-A-TIME — a lock serialises restarts. Concurrent callers wait for
#     the holder instead of racing to kill the same port, then re-run the
#     ALREADY-ACTIVATED check. N workers landing the same build coalesce into
#     a single bounce.
#
# A changed build is activated at once. There is no rate limit (owner,
# 2026-10-12): the broker owns every seat, so a bounce re-adopts the fleet
# rather than restarting it, and holding a landed fix back cost more than
# the bounces it saved. The 🎯T815 activation gate and owner-only --force
# were removed with it.
#
# SELF-DETACH (🎯T405) — being invoked wrongly cannot cause an outage.
#
# This script kills the development daemon, and the daemon's shutdown stops
# every agent — including the agent that invoked the restart. On
# 2026-08-10 the script died with its invoker five seconds after the
# kill, before it reached the step that starts the replacement, and the
# fleet stayed down until the owner opened the cockpit and found it dead.
#
# The hazard was documented here from the first version, as an
# instruction to callers: invoke me detached. A correctness property that
# depends on every caller remembering a convention is not a property, and
# the one caller who forgot took the fleet down.
#
# So the first thing the script does is re-exec itself through bin/detach,
# which runs it in a fresh session (setsid) with its output on a file
# rather than an inheritable pipe. Nothing aimed at the caller's process
# group — SIGHUP, SIGTERM, SIGKILL — reaches the bounce. The caller still
# waits and still gets the exit status; if the caller dies, the bounce
# finishes anyway with nobody to report to.
#
# BLESSED INVOKE (fleet agent / overseer) is unchanged and still
# preferred, because it also stops the caller BLOCKING on the bounce:
#   nohup "$REPO/scripts/restart-jevonsd.sh" \
#     >>"$HOME/.jevons/restart-jevonsd.log" 2>&1 &
# Prefer nohup (portable). If setsid is available:
#   setsid "$REPO/scripts/restart-jevonsd.sh" \
#     >>"$HOME/.jevons/restart-jevonsd.log" 2>&1 < /dev/null &
#
# The daemon itself is also started under nohup/setsid so it outlives
# this script (reparented to init).
#
# SUPERVISION (🎯T405 / 🎯T1111): supervisord holds the development daemon
# (supervisor/jevonsd.ini, autorestart=true) and relaunches it when it
# exits; bin/jevons-watchdog is the interval probe that restarts the daemon
# through this script when a bounce did not take, and tells the owner.
# launchd is not involved.
#
# macOS bash 3.2 safe (no mapfile/associative arrays/bash-4isms).
#
# Usage:
#   ./scripts/restart-jevonsd.sh
#   ./scripts/restart-jevonsd.sh --dry-run
#   ./scripts/restart-jevonsd.sh -h | --help
#   JEVONS_RESTART_AUTH_ID=<id> ./scripts/restart-jevonsd.sh  # auto-frontier only; PO grants via scripts/restart-auth.sh grant <shared-repo> <seat> <target> <base> <head>
#
# Env overrides:
#   JEVONS_RESTART_PORT     default 13705
#   JEVONS_RESTART_WORKDIR  default: repo root
#   JEVONS_RESTART_LOG      default: ~/.local/var/log/jevonsd.log on :13705
#   JEVONS_RESTART_WAIT_SEC default 90
#   JEVONS_RESTART_STOP_WAIT_SEC  grace for the 🎯T392.5 upgrade exit to
#                               release the port before SIGKILL, default 20
#   JEVONS_RESTART_SKIP_MAKE=1  skip rebuild (use existing bin/jevonsd)
#   JEVONS_RESTART_LOCK_WAIT_SEC     wait for an in-flight restart, default 240
#   JEVONS_RESTART_ACTIVE   "<pid> <sha256>" of the daemon we started
#   JEVONS_RESTART_BIN      daemon binary to start (default: $REPO/bin/jevonsd)
#   JEVONS_RESTART_NO_DETACH=1  skip the 🎯T405 self-detach re-exec
#   JEVONS_RESTART_DETACH_LOG   where the detached run writes (default
#                               ~/.jevons/restart-jevonsd.log)
#   JEVONS_RESTART_FAULT=after-kill  test seam: die between freeing the
#                               port and starting the daemon (🎯T405 oracle)
#   JEVONS_RESTART_SUPERVISOR_CONF  supervisord config to drive through
#                               `supervisorctl -c` (🎯T1086). Unset: the
#                               default supervisorctl, development port only.
#                               Set: that supervisord on any port — the
#                               oracle's private one, never the owner's.
#   JEVONS_RESTART_SUPERVISOR_PROGRAM  program name, default jevonsd
#   JEVONS_RESTART_RELAUNCH_WAIT_SEC   how long a supervised upgrade exit may
#                               go without a new pid before this script
#                               gives up, default 20 (🎯T1086)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DAILY_PORT=13705
PORT="${JEVONS_RESTART_PORT:-$DAILY_PORT}"

# 🎯T1062: gate at the script entry, BEFORE worktree handoff, detach, lock,
# build, or signal. A missing/unreadable host checker fails closed for an
# auto-frontier identity, including direct Bash/nohup invocations. This is
# accidental-misuse prevention, NOT complete containment: same-UID workers
# can bypass this script and directly signal/write daemon artifacts.
RESTART_GUARD_WORK=1
for _arg in "$@"; do
  case "$_arg" in -h|--help|--dry-run) RESTART_GUARD_WORK=0 ;; esac
done
if [[ "$PORT" == "$DAILY_PORT" && "$RESTART_GUARD_WORK" == 1 && "${JEVONS_AGENT:-}" =~ ^jv-t[0-9]+(\.[0-9]+)*-auto$ ]]; then
  if [[ ! -x "$ROOT/scripts/restart-auth.sh" ]]; then
    echo "restart-jevonsd: refusing auto-frontier restart: host checker missing (NOT complete containment: same-UID workers can directly signal/write daemon artifacts)" >&2
    exit 3
  fi
  # A linked worktree will hand off to the shared clone. Scope the permit to
  # that clone's HEAD, not to unlanded worktree changes.
  guard_common="$(git -C "$ROOT" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  guard_root="$ROOT"
  if [[ -n "$guard_common" && "$guard_common" != "$(git -C "$ROOT" rev-parse --path-format=absolute --git-dir 2>/dev/null || true)" ]]; then
    guard_root="$(dirname "$guard_common")"
  fi
  "$ROOT/scripts/restart-auth.sh" check "$guard_root" || exit 3
fi

# 🎯T1165: the development daemon serves the shared branch (🎯T505/🎯T553.1),
# never a worker's worktree. ROOT is wherever this copy of the script lives,
# so a worker running its own worktree's copy used to relaunch :13705 on its
# unlanded branch (2026-10-22 11:14, jv-t609-inspect-tail). From a linked
# worktree, hand the development restart to the main working tree's script.
if [ "$PORT" = "$DAILY_PORT" ] && [ -z "${JEVONS_RESTART_WORKDIR:-}" ]; then
  git_dir="$(git -C "$ROOT" rev-parse --path-format=absolute --git-dir 2>/dev/null || true)"
  common_dir="$(git -C "$ROOT" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  if [ -n "$git_dir" ] && [ -n "$common_dir" ] && [ "$git_dir" != "$common_dir" ]; then
    main_root="$(dirname "$common_dir")"
    echo "restart-jevonsd: invoked from linked worktree $ROOT; restarting the development daemon from the shared clone $main_root (🎯T1165)" >&2
    exec "$main_root/scripts/restart-jevonsd.sh" "$@"
  fi
fi
WORKDIR="${JEVONS_RESTART_WORKDIR:-$ROOT}"
# 🎯T771: development daemon boot lines belong in supervisord's capture file.
if [ -n "${JEVONS_RESTART_LOG:-}" ]; then
  LOG="$JEVONS_RESTART_LOG"
elif [ "$PORT" = "$DAILY_PORT" ]; then
  LOG="$HOME/.local/var/log/jevonsd.log"
else
  LOG="$HOME/.jevons/daily-jevonsd.log"
fi
WAIT_SEC="${JEVONS_RESTART_WAIT_SEC:-90}"
# 🎯T392.5: how long to let the upgrade exit (SIGHUP) release the port
# before escalating to SIGKILL. See kill_port_listeners.
STOP_WAIT_SEC="${JEVONS_RESTART_STOP_WAIT_SEC:-20}"
# 🎯T218: identity of the daemon this script last started ("<pid> <sha256>"),
# so a caller whose build is already serving can no-op instead of bouncing.
ACTIVE_FILE="${JEVONS_RESTART_ACTIVE:-$HOME/.jevons/restart-jevonsd.active}"
# 🎯T392.5: serialise concurrent restarts under one exclusive lock.
#
# This replaces a mkdir-based mutex that raced. Between `mkdir` succeeding
# and the holder writing its pid file, a second caller read an empty pid,
# concluded the live holder was stale, rm -rf'd the lock and proceeded.
# On 2026-08-09 five restarts fired in 17 minutes, the last two 59s apart;
# the second killed the first mid-flight and left the development daemon down.
# Every agent turn in flight was cancelled, and cancelled turns bill a full
# context for discarded work — 6.6% of the 🎯T392 baseline.
#
# flock(2) has no such window and the kernel releases it when the holder
# dies, so a crashed run cannot wedge the fleet. Locking is a Go binary
# because a mutex in shell is a race per line (shared bash doctrine).
LOCK_FILE="${JEVONS_RESTART_LOCK:-$HOME/.jevons/restart-jevonsd.lock}"
LOCK_WAIT_SEC="${JEVONS_RESTART_LOCK_WAIT_SEC:-240}"
RUNLOCK="$ROOT/bin/runlock"
# 🎯T254.2: the daemon is compiled from a detached worktree at HEAD, not from
# the shared clone. Kept between runs so the Go build cache stays warm.
BUILDSNAP="$ROOT/bin/buildsnap"
CLAUDIAPIN="$ROOT/bin/claudiapin"
# 🎯T580: the identity of the *sources* the daemon was built from — this
# repo's HEAD/dirty plus every go.work sibling's. The binary hash alone
# called a claudia-only fix "already activated" on 2026-08-29.
BUILDIDENT="$ROOT/bin/buildident"
BOUNCETIMING="$ROOT/bin/bouncetiming"
SNAP_DIR="${JEVONS_RESTART_SNAP_DIR:-$HOME/.jevons/build-snapshot}"

# 🎯T405 SELF-DETACH: re-exec into our own session before anything else,
# so the caller's death cannot cancel the bounce. Outermost on purpose —
# the lock below must be held from inside the detached session, or a
# killed caller would take the lock holder with it.
#
# Fails closed like the lock: a restart that can still be killed by its
# own caller is the failure this removes, so an unbuildable helper aborts
# rather than quietly restoring the old behaviour.
DETACH="$ROOT/bin/detach"
DETACH_LOG="${JEVONS_RESTART_DETACH_LOG:-$HOME/.jevons/restart-jevonsd.log}"

# --help and --dry-run touch nothing, so they neither detach nor take the
# lock: introspection that blocks behind a live restart, or that appends
# to the restart log, is a trap for anyone debugging one.
WANTS_WORK=1
WANTS_HELP=0
for _arg in ${@+"$@"}; do
  case "$_arg" in
    -h|--help) WANTS_HELP=1; WANTS_WORK=0 ;;
    --dry-run) WANTS_WORK=0 ;;
  esac
done

# 🎯T1103 — host-sentinel pause contract. An owner-authorised restart must
# not be raced by a host sentinel acting on a stale pause order: on
# 2026-10-21 the owner approved re-enabling the fleet and this script
# served a fresh daemon at 09:01, and at 09:05 a separate host sentinel
# (~/.local/bin/sen), holding a pause order from 2026-10-15 that nothing
# here knew about, stopped it again and reported the restart as an
# incident. Fixed by hand; this check is the standing fix: before this
# script touches anything (even the --dry-run plan, since both sides must
# agree the marker is gone before invoking it), look for the sentinel's
# pause marker. Present ⇒ refuse with the marker's own text and the exact
# override, never race it. The contract — what writes the marker, what
# removes it, and when a running paused-but-autostarted daemon is the
# owner's action rather than an incident — is docs/design/sentinel-pause-contract.md.
#
# -h/--help is exempt: reading the manual is never something a pause
# should block.
PAUSE_MARKER="${JEVONS_RESTART_PAUSE_MARKER:-$HOME/.local/share/sen/jevons-paused.txt}"
PAUSE_OVERRIDE="${JEVONS_RESTART_OVERRIDE_PAUSE:-0}"
if [[ "$WANTS_HELP" != "1" && -f "$PAUSE_MARKER" && "$PAUSE_OVERRIDE" != "1" ]]; then
  {
    echo "restart-jevonsd: refusing — host-sentinel pause marker present at $PAUSE_MARKER:"
    sed 's/^/  /' "$PAUSE_MARKER"
    echo ""
    echo "This script will not race the sentinel. To lift the pause, follow the"
    echo "sentinel's own unpause step (docs/design/sentinel-pause-contract.md) —"
    echo "do not delete the marker by hand from here."
    echo "To proceed anyway on the owner's explicit word:"
    echo "  JEVONS_RESTART_OVERRIDE_PAUSE=1 $0 ${*:+"$@"}"
  } >&2
  exit 3
fi

# 🎯T553: every successful run appends "served <epoch> <sha> bounce=<0|1>"
# to $SERVED_FILE, naming the committed HEAD it built and whether it
# bounced. internal/t553oracle reads it to tie what :$PORT serves to a commit.
# 🎯T1184: that SHA is the tree the *running* binary was launched from, not
# git HEAD of $ROOT — a worktree build can never pass as HEAD again.
SERVED_FILE="${JEVONS_RESTART_SERVED:-$HOME/.jevons/restart-jevonsd.served}"

served_head() { git -C "$ROOT" rev-parse HEAD 2>/dev/null || printf 'unknown'; }

if [[ "$WANTS_WORK" == "1" && "${JEVONS_RESTART_DETACHED:-0}" != "1" && "${JEVONS_RESTART_NO_DETACH:-0}" != "1" ]]; then
  if [[ ! -x "$DETACH" ]]; then
    (cd "$ROOT" && go build -o "$DETACH" ./cmd/detach) || {
      echo "restart-jevonsd: cannot build $DETACH — refusing to restart where the caller's death can cancel it" >&2
      exit 2
    }
  fi
  export JEVONS_RESTART_DETACHED=1
  exec "$DETACH" -log "$DETACH_LOG" -- "$0" "$@"
fi

# Re-exec under the lock unless we are already the locked child. Fails
# closed: restarting unserialised is the failure mode this exists to stop,
# so a missing runlock is built, and an unbuildable one aborts.
if [[ "$WANTS_WORK" == "1" && "${JEVONS_RESTART_LOCKED:-0}" != "1" && "${JEVONS_RESTART_NO_LOCK:-0}" != "1" ]]; then
  if [[ ! -x "$RUNLOCK" ]]; then
    (cd "$ROOT" && go build -o "$RUNLOCK" ./cmd/runlock) || {
      echo "restart-jevonsd: cannot build $RUNLOCK — refusing to restart unserialised" >&2
      exit 2
    }
  fi
  export JEVONS_RESTART_LOCKED=1
  exec "$RUNLOCK" -timeout "${LOCK_WAIT_SEC}s" "$LOCK_FILE" "$0" "$@"
fi

BIN="${JEVONS_RESTART_BIN:-$ROOT/bin/jevonsd}"
DRY_RUN=0
# 🎯T1086: which supervisord this script may drive, and how.
SUPERVISOR_CONF="${JEVONS_RESTART_SUPERVISOR_CONF:-}"
SUPERVISOR_PROGRAM="${JEVONS_RESTART_SUPERVISOR_PROGRAM:-jevonsd}"
RELAUNCH_WAIT_SEC="${JEVONS_RESTART_RELAUNCH_WAIT_SEC:-20}"
SUPERVISORPOLICY="$ROOT/bin/supervisorpolicy"
SKIP_MAKE="${JEVONS_RESTART_SKIP_MAKE:-0}"

usage() {
  cat <<'EOF'
Usage: restart-jevonsd.sh [options]

Rebuild bin/jevonsd, stop the brew service if needed, free the development port,
relaunch under supervisord (or start repo bin/jevonsd detached where nothing
supervises the port), wait until /health and /api/frontier serve.

🎯T218: if the rebuilt binary is already the one the running daemon is
serving, this exits 0 without touching the port (already activated).
Concurrent callers serialise on a lock and coalesce. A changed binary is
activated at once; there is no rate limit.

Options:
  -h, --help     Show this help and exit 0
  --dry-run      Print planned steps; do not stop/start/kill

Env:
  JEVONS_RESTART_PORT              Listen port (default 13705)
  JEVONS_RESTART_WORKDIR           -workdir for jevonsd (default: repo root)
  JEVONS_RESTART_LOG               Daemon log (default ~/.local/var/log/jevonsd.log on :13705)
  JEVONS_RESTART_WAIT_SEC          Health wait seconds (default 90)
  JEVONS_RESTART_STOP_WAIT_SEC     Grace for the upgrade exit to free the port
                                   before SIGKILL (default 20; 🎯T392.5)
  JEVONS_RESTART_SKIP_MAKE         If 1, skip make rebuild
  JEVONS_RESTART_LOCK_WAIT_SEC     Wait for in-flight restart (default 240; 🎯T218)
  JEVONS_RESTART_ACTIVE            Running-daemon identity (default ~/.jevons/restart-jevonsd.active)
  JEVONS_RESTART_LOCK              Lock dir (default ~/.jevons/restart-jevonsd.lock)
  JEVONS_RESTART_BIN               Daemon binary (default $REPO/bin/jevonsd)
  JEVONS_RESTART_NO_DETACH         If 1, skip the 🎯T405 self-detach re-exec
  JEVONS_RESTART_DETACH_LOG        Detached run's log (default ~/.jevons/restart-jevonsd.log)
  JEVONS_RESTART_SUPERVISOR_CONF   supervisord config for supervisorctl -c (🎯T1086)
  JEVONS_RESTART_SUPERVISOR_PROGRAM  supervisord program (default jevonsd)
  JEVONS_RESTART_RELAUNCH_WAIT_SEC   Wait for a supervised relaunch (default 20)
  JEVONS_RESTART_PAUSE_MARKER      Host-sentinel pause marker file (🎯T1103;
                                   default ~/.local/share/sen/jevons-paused.txt)
  JEVONS_RESTART_OVERRIDE_PAUSE    If 1, proceed despite the pause marker (owner only)

SELF-DETACH (🎯T405): this script re-execs itself through bin/detach into
its own session before doing anything, so the caller's death — including
the agent that this restart's own kill is about to stop — cannot cancel
the bounce. A caller that is still alive still gets the exit status.

BLESSED INVOKE (survive agent/overseer death AND stop the caller blocking
on the bounce; the self-detach above covers the first half on its own):
  nohup ./scripts/restart-jevonsd.sh >>"$HOME/.jevons/restart-jevonsd.log" 2>&1 &

SUPERVISION (🎯T405 / 🎯T1111): supervisord holds the development daemon
(supervisor/jevonsd.ini) and relaunches it on exit; bin/jevons-watchdog is
the interval probe that calls this script when a bounce did not take.
EOF
}

log() {
  printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*"
}

die() {
  log "ERROR: $*"
  exit 1
}

# 🎯T794: separate pre-shutdown work (old daemon still serving) from outage downtime.
log_t794_pre_shutdown() {
  local phase="$1"
  local detail="${2:-}"
  if [[ -n "${BOUNCE_WORK_START:-}" ]]; then
    local now elapsed
    now=$(date +%s)
    elapsed=$((now - BOUNCE_WORK_START))
    if [[ -n "$detail" ]]; then
      log "🎯T794 pre-shutdown ($phase, ${elapsed}s since bounce work began; old daemon still serving until upgrade exit): $detail"
    else
      log "🎯T794 pre-shutdown ($phase, ${elapsed}s since bounce work began; old daemon still serving until upgrade exit)"
    fi
  else
    log "🎯T794 pre-shutdown ($phase): $detail"
  fi
}

report_t794_bounce_timing() {
  local health_epoch="${1:-}"
  if [[ ! -x "$BOUNCETIMING" ]]; then
    (cd "$ROOT" && go build -o "$BOUNCETIMING" ./cmd/bouncetiming) >/dev/null 2>&1 || true
  fi
  if [[ ! -x "$BOUNCETIMING" || ! -r "$LOG" ]]; then
    log "🎯T794 bounce timing: daemon log unreadable; skipped"
    return 0
  fi
  local health_at=""
  if [[ -n "$health_epoch" ]]; then
    health_at=$(date -r "$health_epoch" '+%Y-%m-%dT%H:%M:%S%z' 2>/dev/null || true)
  fi
  local out
  if [[ -n "$health_at" ]]; then
    out="$("$BOUNCETIMING" -daemon-log "$LOG" -health-at "$health_at" 2>&1)" || true
  else
    out="$("$BOUNCETIMING" -daemon-log "$LOG" 2>&1)" || true
  fi
  if [[ -n "${BOUNCE_WORK_START:-}" && -n "${BOUNCE_SHUTDOWN_EPOCH:-}" ]]; then
    local pre=$((BOUNCE_SHUTDOWN_EPOCH - BOUNCE_WORK_START))
    log "🎯T794 bounce timing: pre_shutdown_build=${pre}s (script work while old daemon served) $out"
  else
    log "🎯T794 bounce timing: $out"
  fi
}

# 🎯T1086 / 🎯T405: tell the owner out of band that this run is leaving the
# daemon down. The cockpit is what is missing during an outage, so the
# notice goes through blurter, a separate daemon, as the watchdog's do.
# Same key as the watchdog's outage notice, so both speak of one outage.
# Best effort: no blurter is logged, never a reason to change the exit.
notify_owner_down() {
  local reason="$1" blurter
  blurter="$(command -v blurter 2>/dev/null || true)"
  if [[ -z "$blurter" ]]; then
    log "no blurter on PATH; the owner is not told that :$PORT is down"
    return 0
  fi
  "$blurter" send \
    --app restart-jevonsd \
    --severity problem \
    --key "jevonsd-down-$PORT" \
    --subject "jevons daemon down on :$PORT" \
    --body "restart-jevonsd is leaving the development jevonsd down: $reason (daemon log: $LOG)" \
    --quiet >/dev/null 2>&1 || log "blurter notice failed"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    *)
      die "unknown argument: $1 (try --help)"
      ;;
  esac
done

# --- helpers -----------------------------------------------------------------

# --- 🎯T218 thrash policy helpers --------------------------------------------

binary_sha() {
  # sha256 of $1, bare hex. macOS ships shasum; Linux ships sha256sum.
  local f="$1"
  [[ -r "$f" ]] || return 1
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$f" 2>/dev/null | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" 2>/dev/null | awk '{print $1}'
  else
    return 1
  fi
}

running_sha() {
  # Hash of the binary the *currently listening* daemon was started from,
  # or empty when we cannot prove it. Empty always means "restart needed" —
  # the safe default, never a silent skip.
  #
  # The active stamp records the pid we started plus the hash it ran. It is
  # only trustworthy while that pid is still the process holding :$PORT; if
  # anything else (brew, make run, a hand-start) took the port, the recorded
  # hash says nothing about what is serving, so we report unknown.
  [[ -f "$ACTIVE_FILE" ]] || return 0
  local recorded_pid recorded_sha listeners
  recorded_pid="$(awk '{print $1}' "$ACTIVE_FILE" 2>/dev/null || true)"
  recorded_sha="$(awk '{print $2}' "$ACTIVE_FILE" 2>/dev/null || true)"
  [[ -n "$recorded_pid" && -n "$recorded_sha" ]] || return 0
  listeners="$(list_listen_pids)"
  [[ -n "$listeners" ]] || return 0
  # Exactly the recorded pid must hold the port — a set that merely includes
  # it (or a different pid entirely) is not proof of what serves.
  if [[ "$(echo "$listeners" | tr -d '[:space:]')" != "$recorded_pid" ]]; then
    return 0
  fi
  printf '%s' "$recorded_sha"
}

running_ident() {
  # 🎯T580: the source identity recorded for the daemon holding :$PORT
  # (third field of the active stamp), or empty when we cannot prove it.
  # Same pid discipline as running_sha: a stamp whose pid no longer owns
  # the port says nothing about what serves.
  [[ -f "$ACTIVE_FILE" ]] || return 0
  local recorded_pid recorded_ident listeners
  recorded_pid="$(awk '{print $1}' "$ACTIVE_FILE" 2>/dev/null || true)"
  recorded_ident="$(awk '{print $3}' "$ACTIVE_FILE" 2>/dev/null || true)"
  [[ -n "$recorded_pid" && -n "$recorded_ident" ]] || return 0
  listeners="$(list_listen_pids)"
  [[ -n "$listeners" ]] || return 0
  if [[ "$(echo "$listeners" | tr -d '[:space:]')" != "$recorded_pid" ]]; then
    return 0
  fi
  printf '%s' "$recorded_ident"
}

source_identity() {
  # Bare hex identity from bin/buildident, or empty. Empty degrades this
  # run to the binary hash alone — narrower, and logged as such — never a
  # hard failure: a pristine clone, a snapshot worktree and the hermetic
  # fixtures all legitimately have no workspace and sometimes no toolchain.
  if [[ ! -x "$BUILDIDENT" ]]; then
    command -v go >/dev/null 2>&1 || return 0
    (cd "$ROOT" && go build -o "$BUILDIDENT" ./cmd/buildident) >/dev/null 2>&1 || return 0
  fi
  "$BUILDIDENT" -root "$ROOT" -id 2>/dev/null | head -n1
}

http_code() {
  # Prints 000 on total failure (curl missing network, refused, etc.).
  local url="$1"
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' --connect-timeout 2 --max-time 5 "$url" 2>/dev/null || true)"
  if [[ -z "$code" ]]; then
    code="000"
  fi
  printf '%s' "$code"
}

list_listen_pids() {
  # macOS/BSD lsof: PIDs listening on TCP $PORT, one per line.
  lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null || true
}

kill_port_listeners() {
  # 🎯T392.5: ASK FOR AN UPGRADE EXIT, NOT A DRAIN.
  #
  # This script bounces the daemon on every daemon-path land, by design
  # (🎯T188/🎯T191 make restarts routine and unattended, and that must
  # stay). What it must not do is charge the fleet for the privilege.
  #
  # jevonsd has told two different stories apart since 🎯T40:
  #
  #   SIGINT/SIGTERM → ModeNormal  → registry.StopAll(). Every agent
  #     process is stopped, so every turn in flight is cancelled. A
  #     cancelled turn has already billed its whole context and produces
  #     nothing — 120 of 1,070 turns and 59.7M input tokens (6.6%) went
  #     this way in the 🎯T392 baseline, with daemon restarts landing
  #     mid-flight one of the two dominant sources.
  #
  #   SIGHUP        → ModeUpgrade → StopAll is SKIPPED and reattach
  #     handles are written (internal/upgrade). Agents are detached
  #     `grok agent serve` processes (CLAUDIA_GROK_CONNECT=1, set by the
  #     daemon itself) precisely so they outlive the coordinator, and
  #     the successor merges the handles back in at boot.
  #
  # The upgrade path was built for exactly this and this script never
  # asked for it: a plain `kill` is SIGTERM, so every routine bounce took
  # the drain. One signal is the whole fix — the turns keep running
  # across the bounce and reattach to the new coordinator.
  #
  # ESCALATION. SIGHUP is a request; freeing the port is still mandatory
  # (🎯T194 — a restart that reports success while the old binary serves
  # is the failure this script exists to prevent). So we poll for release
  # up to STOP_WAIT_SEC and then SIGKILL. There is deliberately no SIGTERM
  # rung between them: the daemon's handler reads ONE signal off sigCh and
  # returns, so a second signal after SIGHUP is never read and cannot help.
  # Reaching SIGKILL means the upgrade exit was already wedged, and the
  # agents survive it anyway (they are not in this process group) — they
  # just lose the reattach handles the graceful path would have written.
  #
  # NOT the owner's stop button. This is the restart script's own signal
  # to the coordinator. Owner cancellation is a different path entirely
  # (agent kill/stop, the 🎯T36 killswitch) and is untouched here: it stays
  # immediate and unconditional, which is a hard constraint of 🎯T392.5.
  local pids waited
  pids="$(list_listen_pids)"
  if [[ -z "$pids" ]]; then
    log "no listeners on :$PORT"
    return 0
  fi
  BOUNCE_SHUTDOWN_EPOCH=$(date +%s)
  log_t794_pre_shutdown end "requesting upgrade exit; owner-visible downtime begins after old process exits"
  log "🎯T392.5 upgrade-exit (SIGHUP) to listeners on :$PORT: $(echo "$pids" | tr '\n' ' ') — in-flight agent turns keep running across the bounce"
  # Word-split is intentional: kill accepts multiple PIDs.
  # shellcheck disable=SC2086
  kill -HUP $pids 2>/dev/null || true

  # Poll rather than sleep a fixed grace: the upgrade exit closes the
  # listener and then writes handles, so the port usually frees in well
  # under a second and waiting the whole window would be pure latency
  # added to every land.
  waited=0
  while [[ "$waited" -lt "$STOP_WAIT_SEC" ]]; do
    pids="$(list_listen_pids)"
    [[ -n "$pids" ]] || break
    sleep 1
    waited=$((waited + 1))
  done

  pids="$(list_listen_pids)"
  if [[ -n "$pids" ]]; then
    log "🎯T392.5 upgrade exit did not free :$PORT within ${STOP_WAIT_SEC}s; force-killing: $(echo "$pids" | tr '\n' ' ')"
    # shellcheck disable=SC2086
    kill -9 $pids 2>/dev/null || true
    sleep 0.5
  fi
  pids="$(list_listen_pids)"
  if [[ -n "$pids" ]]; then
    die "port :$PORT still held by: $(echo "$pids" | tr '\n' ' ')"
  fi
}



stop_brew_jevons() {
  # Cellar launchd KeepAlive will reclaim :13705 if brew service is loaded.
  # Stop it whenever brew is present so repo bin/jevonsd owns the development port.
  #
  # Only for the development port: the Cellar service is configured for :13705 and
  # can never be holding a throwaway one, so stopping it on behalf of a test
  # bounce would take the owner's daemon down to fix a port it does not have
  # (🎯T405 — the oracle drives this script on a scratch port).
  if [[ "$PORT" != "$DAILY_PORT" ]]; then
    log "port :$PORT is not the development :$DAILY_PORT; leaving brew services alone"
    return 0
  fi
  if ! command -v brew >/dev/null 2>&1; then
    log "brew not on PATH; skip brew services stop"
    return 0
  fi
  if brew services list 2>/dev/null | grep -E '^jevons[[:space:]]' >/dev/null 2>&1; then
    log "brew services stop jevons (prevent Cellar reclaim of :$PORT)"
    brew services stop jevons >/dev/null 2>&1 || true
  else
    log "brew services: no jevons formula listed; nothing to stop"
  fi
}

# 🎯T1086: every supervisorctl call goes through here, so a run told to use
# a private supervisord (the oracle's) can never reach the owner's.
sctl() {
  if [[ -n "$SUPERVISOR_CONF" ]]; then
    supervisorctl -c "$SUPERVISOR_CONF" "$@"
  else
    supervisorctl "$@"
  fi
}

# 🎯T1086: what supervisord's config says it does when the program exits,
# as one line ("autorestart=false autostart=false file=…"), or
# "policy unknown: …". supervisorctl cannot show autorestart, so
# bin/supervisorpolicy reads the config the running supervisord loaded
# (its -c argument) and follows [include] to the program's section.
supervisor_policy() {
  local out spid
  if [[ ! -x "$SUPERVISORPOLICY" ]]; then
    (cd "$ROOT" && go build -o "$SUPERVISORPOLICY" ./cmd/supervisorpolicy) >/dev/null 2>&1 || {
      printf 'policy unknown: cannot build %s' "$SUPERVISORPOLICY"
      return 0
    }
  fi
  if [[ -n "$SUPERVISOR_CONF" ]]; then
    out="$("$SUPERVISORPOLICY" -program "$SUPERVISOR_PROGRAM" -conf "$SUPERVISOR_CONF" 2>&1)" || true
  else
    spid="$(sctl pid 2>/dev/null || true)"
    if [[ ! "$spid" =~ ^[1-9][0-9]*$ ]]; then
      printf 'policy unknown: supervisorctl pid gave no supervisord pid'
      return 0
    fi
    out="$("$SUPERVISORPOLICY" -program "$SUPERVISOR_PROGRAM" -supervisord-pid "$spid" 2>&1)" || true
  fi
  printf '%s' "${out:-policy unknown: no output from $SUPERVISORPOLICY}"
}

# 🎯T1086: after the upgrade exit, make sure something launches the
# replacement. supervisord does when the program relaunches on exit; when
# it does not (autorestart=false or unexpected — a paused config is a
# legitimate owner state, not a broken one) the program lands in EXITED,
# and this script starts it, restoring the RUNNING state it found. The
# check is the program's state, not the policy line: the file on disk can
# differ from what supervisord loaded until `supervisorctl update`.
#
# FATAL is started too: supervisord tried to relaunch and gave up. STOPPED
# is not: that is someone's explicit stop, and overriding it is not this
# script's call.
ensure_supervised_relaunch() {
  local old_pid="$1" policy="$2" wait_sec="${3:-$RELAUNCH_WAIT_SEC}" ticks pid status started=0
  ticks=$((wait_sec * 5))
  while [[ "$ticks" -gt 0 ]]; do
    pid="$(sctl pid "$SUPERVISOR_PROGRAM" 2>/dev/null || true)"
    if [[ "$pid" =~ ^[1-9][0-9]*$ && "$pid" != "$old_pid" ]]; then
      if [[ "$started" -eq 1 ]]; then
        log "🎯T1086 started $SUPERVISOR_PROGRAM through supervisord: pid=$pid"
      else
        log "supervisord relaunched $SUPERVISOR_PROGRAM: pid=$pid"
      fi
      return 0
    fi
    status="$(sctl status "$SUPERVISOR_PROGRAM" 2>/dev/null || true)"
    case "$status" in
      *EXITED*|*FATAL*)
        if [[ "$started" -eq 0 ]]; then
          log "🎯T1086 supervisord left $SUPERVISOR_PROGRAM down after the upgrade exit ($(printf '%s' "$status" | tr -s ' ' | cut -c1-120); $policy); starting it"
          started=1
          sctl start "$SUPERVISOR_PROGRAM" ||
            log "🎯T1086 supervisorctl start $SUPERVISOR_PROGRAM failed; still waiting for a pid"
        fi
        ;;
    esac
    sleep 0.2
    ticks=$((ticks - 1))
  done
  notify_owner_down "supervisord program $SUPERVISOR_PROGRAM has no new pid ${wait_sec}s after its upgrade exit ($policy)"
  die "🎯T1086 supervisord program $SUPERVISOR_PROGRAM did not come back within ${wait_sec}s of its upgrade exit ($policy)"
}

# T540.2: preserve the existing supervisor owner during React activation.
# SIGHUP requests the daemon's fleet-preserving upgrade exit. Do not race
# supervisord's replacement with the legacy kill-port/nohup adoption path.
#
# 🎯T1086: on 2026-10-20 this signalled a program whose config said
# autorestart=false (left by a pause) and then waited 90 s for a daemon
# nothing would start. It now reads the policy before signalling, says
# which way the bounce will complete, and completes it either way.
upgrade_with_supervisor() {
  # The owner's supervisord serves only the development port. A run on any
  # other port uses a supervisord only when told which one, by config path.
  if [[ -z "$SUPERVISOR_CONF" ]]; then
    [[ "$PORT" == "$DAILY_PORT" ]] || return 1
  fi
  command -v supervisorctl >/dev/null 2>&1 || return 1
  local managed_pid listeners policy
  managed_pid="$(sctl pid "$SUPERVISOR_PROGRAM" 2>/dev/null)" || return 1
  [[ "$managed_pid" =~ ^[1-9][0-9]*$ ]] || return 1
  listeners="$(list_listen_pids)"
  [[ "$listeners" == "$managed_pid" ]] || return 1
  policy="$(supervisor_policy)"
  log "React upgrade through existing supervisord owner pid=$managed_pid ($policy)"
  case "$policy" in
    autorestart=true\ *)
      log "🎯T1086 supervisord relaunches $SUPERVISOR_PROGRAM on exit; this script starts it only if supervisord leaves it EXITED or FATAL" ;;
    *)
      log "🎯T1086 supervisord will not relaunch $SUPERVISOR_PROGRAM on an upgrade exit ($policy); this script starts it after the exit" ;;
  esac
  BOUNCE_SHUTDOWN_EPOCH=$(date +%s)
  log_t794_pre_shutdown end "supervised upgrade exit; owner-visible downtime begins after old process exits"
  sctl signal HUP "$SUPERVISOR_PROGRAM" || die "supervisor could not request upgrade exit"
  # 🎯T770: the supervised upgrade handshake is not "old pid exited within
  # STOP_WAIT_SEC". On 2026-09-21 the script died here while supervisord
  # relaunched ~30s later. Wait for the relaunch success condition instead,
  # budgeting STOP_WAIT_SEC + RELAUNCH_WAIT_SEC for a slow upgrade exit.
  ensure_supervised_relaunch "$managed_pid" "$policy" "$((STOP_WAIT_SEC + RELAUNCH_WAIT_SEC))"
  return 0
}


start_daemon_detached() {
  # Detach so SIGHUP/SIGTERM to this script (or its parent agent) does not
  # kill the new jevonsd. Prefer setsid when present; always nohup.
  mkdir -p "$(dirname "$LOG")"
  touch "$LOG"

  if [[ ! -x "$BIN" ]]; then
    die "binary not executable: $BIN"
  fi

  log "starting detached: $BIN -port $PORT -workdir $WORKDIR (log=$LOG)"

  # 🎯T442: the script exports JEVONS_RESTART_DETACHED=1 / _LOCKED=1 into its
  # own process before the re-execs that honour them. The daemon started here
  # must not inherit those flags — every fleet agent it later spawns would
  # then skip both re-execs when *it* invoked this script, racing the port
  # with every concurrent caller. Clear them in this shell before nohup so
  # the child environ is clean; the script's own later logic has already
  # passed the re-exec gates.
  #
  # 🎯T529: same for JEVONS_RESTART_LOCK / LOCK_WAIT_SEC — LOCK_FILE and
  # LOCK_WAIT_SEC were already snapshotted into locals above; leaving the
  # env keys set would hand every fleet seat the fleet lock path, and
  # TestRestartThrashPolicy (or a real bounce from an agent) would then
  # contend on ambient state instead of its own fixture/default.
  unset JEVONS_RESTART_DETACHED JEVONS_RESTART_LOCKED
  unset JEVONS_RESTART_NO_DETACH JEVONS_RESTART_NO_LOCK JEVONS_RESTART_FAULT
  unset JEVONS_RESTART_LOCK JEVONS_RESTART_LOCK_WAIT_SEC

  if command -v setsid >/dev/null 2>&1; then
    # Linux: new session; still wrap with nohup for SIGHUP immunity.
    nohup setsid "$BIN" -port "$PORT" -workdir "$WORKDIR" \
      </dev/null >>"$LOG" 2>&1 &
  else
    # macOS (no setsid in stock userland): nohup + background is enough
    # for agent death survival when the *script* was also invoked under nohup.
    nohup "$BIN" -port "$PORT" -workdir "$WORKDIR" \
      </dev/null >>"$LOG" 2>&1 &
  fi
  local pid=$!
  disown "$pid" 2>/dev/null || true
  log "daemon pid=$pid (detached)"
  printf '%s\n' "$pid" >"$HOME/.jevons/daily-jevonsd.pid" 2>/dev/null || true
}

record_active_identity() {
  # 🎯T218: record which pid serves which build, so the next caller can tell
  # "already activated" from "stale binary" without bouncing to find out.
  #
  # Read the pid back from the port rather than trusting $! from the start:
  # `setsid` forks when the caller is already a process-group leader, which
  # would make $! a wrapper that no longer exists. The process actually
  # holding :$PORT is the only pid worth recording.
  #
  # 🎯T892: use $WANT_SHA — the hash decided BEFORE this run started the
  # daemon — never re-hash $BIN here. start_daemon_detached forked the
  # daemon from whatever $BIN held at that moment; re-reading $BIN now is a
  # TOCTOU race against anything that rebuilds $BIN in the gap between
  # "daemon is up" (wait_until_serving, polled externally over HTTP) and
  # this stamp. A caller who observes /health=200 and immediately rebuilds
  # $BIN for its own next bounce would otherwise poison the active stamp
  # with a sha nothing ever actually served — a real daemon serving build
  # "b" gets recorded as "c", and every subsequent already_activated() call
  # wrongly believes "c" is already up. Reproduced by
  # TestRestartThrashPolicy's 4-concurrent-caller case under load.
  local sha listeners ident
  sha="$WANT_SHA"
  listeners="$(list_listen_pids | tr -d '[:space:]')"
  # 🎯T580: third field is the source identity this build came from, so the
  # next caller can see a sibling-only change the hash cannot show. Absent
  # when it could not be computed — never faked, and read back as "unknown".
  ident="${WANT_IDENT:-}"
  if [[ -n "$sha" && -n "$listeners" ]]; then
    mkdir -p "$(dirname "$ACTIVE_FILE")" 2>/dev/null || true
    printf '%s %s %s\n' "$listeners" "$sha" "$ident" >"$ACTIVE_FILE" 2>/dev/null || true
    log "🎯T218 active: pid=$listeners sha=${sha:0:12}… 🎯T580 ident=${ident:0:12}${ident:+…}"
  else
    # Unknown identity → the next caller restarts. Correct, just not
    # thrash-free; never let a missing hash or pid fake activation.
    rm -f "$ACTIVE_FILE" 2>/dev/null || true
    log "🎯T218 active identity unknown (sha=${sha:-none} listeners=${listeners:-none}); next call will restart"
  fi
}

wait_until_serving() {
  local deadline i code fcode
  deadline=$((WAIT_SEC))
  i=0
  log "waiting up to ${deadline}s for /health + /api/frontier on :$PORT"
  while [[ "$i" -lt "$deadline" ]]; do
    code="$(http_code "http://127.0.0.1:${PORT}/health")"
    if [[ "$code" == "200" ]]; then
      fcode="$(http_code "http://127.0.0.1:${PORT}/api/frontier")"
      # Prefer non-404 frontier (API present). Accept 200/other non-404.
      if [[ "$fcode" != "404" && "$fcode" != "000" ]]; then
        log "serving: /health=$code /api/frontier=$fcode"
        return 0
      fi
      log "health ok ($code) but /api/frontier=$fcode; waiting…"
    else
      log "health=$code (not ready); waiting…"
    fi
    sleep 1
    i=$((i + 1))
  done
  notify_owner_down "not serving ${deadline}s after the bounce"
  die "timed out after ${deadline}s waiting for development jevonsd on :$PORT (log=$LOG)"
}

# 🎯T1184: the git working tree the process holding :$PORT was launched
# from. After a worktree's supervisor-install pointed jevonsd.ini at a
# worker tree (2026-10-22 15:46–18:19), every restart relaunched that
# worker's build while this log claimed the shared HEAD. A worktree
# build can never pass as HEAD again.
canon_dir() {
  (CDPATH= cd "$1" && pwd -P)
}

running_binary_path() {
  local pid="$1" exe
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  if [[ -e "/proc/$pid/exe" ]]; then
    exe="$(readlink "/proc/$pid/exe" 2>/dev/null || true)"
    exe="${exe% (deleted)}"
    [[ -n "$exe" ]] || return 1
    printf '%s' "$exe"
    return 0
  fi
  exe="$(lsof -p "$pid" -a -d txt -Fn 2>/dev/null | awk '/^n\// {print substr($0,2); exit}')"
  [[ -n "$exe" ]] || return 1
  printf '%s' "$exe"
}

running_binary_tree() {
  local pid exe dir
  pid="$(list_listen_pids | awk 'NF {print; exit}')"
  exe="$(running_binary_path "$pid")" || return 1
  [[ -e "$exe" ]] || return 1
  dir="$(git -C "$(dirname "$exe")" rev-parse --show-toplevel 2>/dev/null || true)"
  [[ -n "$dir" ]] || return 1
  canon_dir "$dir"
}

record_served() {
  # 🎯T553: one ledger line naming the HEAD this run served.
  # 🎯T1184: if the process on :$PORT lives in a different git tree than
  # $ROOT (a worker worktree), name that tree's HEAD instead of the
  # shared clone's. Same-tree keeps the pre-build capture so a commit
  # landing mid-build stays an under-claim, never an over-claim.
  local bounce="$1" head tree root_tree
  head="$SERVED_HEAD"
  tree="$(running_binary_tree 2>/dev/null || true)"
  root_tree="$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null || true)"
  if [[ -n "$tree" && -n "$root_tree" ]]; then
    root_tree="$(canon_dir "$root_tree")"
    if [[ "$tree" != "$root_tree" ]]; then
      head="$(git -C "$tree" rev-parse HEAD 2>/dev/null || printf '%s' "$head")"
      log "🎯T1184 serving $tree head=${head:0:12} (shared HEAD was ${SERVED_HEAD:0:12})"
    fi
  fi
  mkdir -p "$(dirname "$SERVED_FILE")" 2>/dev/null || true
  printf 'served %s %s bounce=%s\n' "$(date +%s)" "$head" "$bounce" >>"$SERVED_FILE" 2>/dev/null || true
  log "served head=${head:0:12} bounce=$bounce"
}

# --- main --------------------------------------------------------------------

log "🎯T191 restart-jevonsd: root=$ROOT port=$PORT workdir=$WORKDIR dry_run=$DRY_RUN"

if [[ "$DRY_RUN" -eq 1 ]]; then
  log "[dry-run] would: 🎯T254.2 build bin/jevonsd from committed HEAD in $SNAP_DIR (never the shared tree) unless SKIP_MAKE=$SKIP_MAKE"
  log "[dry-run] would: 🎯T218 no-op if the running daemon already serves this build (already activated)"
  log "[dry-run] would: 🎯T405 re-exec through $DETACH into its own session so the caller's death cannot cancel the bounce"
  log "[dry-run] would: 🎯T392.5 hold $LOCK_FILE via runlock so concurrent restarts serialise"
  log "[dry-run] would: brew services stop jevons (if brew lists jevons)"
  log "[dry-run] would: 🎯T392.5 SIGHUP listeners on :$PORT (upgrade exit — in-flight agent turns survive), SIGKILL only if the port is still held after ${STOP_WAIT_SEC}s"
  log "[dry-run] would: nohup/setsid start $BIN -port $PORT -workdir $WORKDIR >>$LOG where nothing supervises the port (🎯T1111)"
  log "[dry-run] would: 🎯T1086 under supervisord, read $SUPERVISOR_PROGRAM's autorestart before SIGHUP and supervisorctl start it if the upgrade exit leaves it EXITED or FATAL"
  log "[dry-run] would: wait for /health 200 and /api/frontier non-404; on timeout tell the owner through blurter"
  log "[dry-run] BLESSED INVOKE: nohup $ROOT/scripts/restart-jevonsd.sh >>\$HOME/.jevons/restart-jevonsd.log 2>&1 &"
  exit 0
fi

# 🎯T553: the HEAD this run serves, read before the build. A commit landing
# mid-build can only make this an under-claim, never an over-claim.
SERVED_HEAD="$(served_head)"

if [[ "$SKIP_MAKE" != "1" ]]; then
  # 🎯T254.2: build committed HEAD in a throwaway worktree, never the shared
  # clone. A dozen fleet workers edit this tree at once, so `make -C $ROOT`
  # compiles whatever any of them has half-written: on 2026-08-09 a restart
  # could not rebuild at all because another worker's uncommitted chat.go
  # called a method that did not exist, and the worker running the restart
  # had no way to activate their own landed change. Uncommitted work is
  # deliberately NOT built — commit, then restart.
  #
  # Fails closed, like the runlock above: building the shared tree as a
  # fallback would silently restore the exact failure this removes.
  if [[ ! -x "$BUILDSNAP" ]]; then
    (cd "$ROOT" && go build -o "$BUILDSNAP" ./cmd/buildsnap) || \
      die "cannot build $BUILDSNAP — refusing to rebuild from the shared tree"
  fi
  log "rebuild: bin/jevonsd from committed HEAD (snapshot $SNAP_DIR)"
  "$BUILDSNAP" -root "$ROOT" -snap "$SNAP_DIR" \
    -target bin/jevonsd -artifact bin/jevonsd -dest "$BIN"


  # 🎯T553.1: React is built, type-checked and embedded by bin/jevonsd in the
  # same committed HEAD snapshot. There is no separately mutable UI to copy.

else
  log "skip make (JEVONS_RESTART_SKIP_MAKE=1)"
  [[ -x "$BIN" ]] || die "no binary at $BIN"
fi

# 🎯T448: prove the go.mod claudia pin against the sibling checkout.
# Names the pin SHA and any sibling commits the pin is missing. Hard-fails
# (die) when a required fleet commit (T28 send-submit) is absent from the
# pin; LOUD-warns when the sibling is merely ahead of a sufficient pin
# (development still builds via ../go.work + buildsnap sibling inject).
if [[ ! -x "$CLAUDIAPIN" ]]; then
  (cd "$ROOT" && go build -o "$CLAUDIAPIN" ./cmd/claudiapin) || \
    die "cannot build $CLAUDIAPIN — refusing a silent claudia pin"
fi
log "🎯T448 claudia pin check:"
pin_rc=0
pin_out="$("$CLAUDIAPIN" "$ROOT" 2>&1)" || pin_rc=$?
printf '%s\n' "$pin_out" | while IFS= read -r line; do log "  $line"; done
if [[ "$pin_rc" -eq 2 ]]; then
  die "🎯T448 claudia pin missing required fleet commits — bump go.mod pin (see bin/claudiapin)"
elif [[ "$pin_rc" -ne 0 ]]; then
  die "🎯T448 claudiapin exited $pin_rc"
fi

# 🎯T218: the build is now current, so we can ask the only question that
# matters — is this binary already serving? Answering it *before* killing
# anything is what turns five bounces into one.
WANT_SHA="$(binary_sha "$BIN" || true)"

# 🎯T580: what the binary hash cannot see. jevons consumes local-master
# claudia through ../go.work (🎯T448), so a sibling commit changes what is
# built while this tree — and, in the 2026-08-29 incident, the compiled
# binary's hash — reports nothing moved: a rebuilt daemon carrying claudia
# f8869fe was declared "already activated" and the pre-fix process kept
# serving until someone passed --force. The identity covers this repo's
# HEAD and dirty state plus every workspace sibling's.
WANT_IDENT="$(source_identity || true)"
if [[ -n "$WANT_IDENT" ]]; then
  log "🎯T580 source identity: ${WANT_IDENT:0:12}…"
  if [[ -x "$BUILDIDENT" ]]; then
    "$BUILDIDENT" -root "$ROOT" 2>/dev/null | while IFS= read -r line; do log "  $line"; done
  fi
else
  log "🎯T580 source identity unavailable; this run compares the binary hash only, so a sibling-only change may read as unchanged"
fi

already_activated() {
  # True when the daemon holding :$PORT runs WANT_SHA, was built from the
  # same sources, and answers /health.
  [[ -n "$WANT_SHA" ]] || return 1
  local have have_ident
  have="$(running_sha)"
  [[ -n "$have" && "$have" == "$WANT_SHA" ]] || return 1
  if [[ -n "$WANT_IDENT" ]]; then
    # 🎯T580: an identity we can compute and cannot match is a bounce.
    # Unknown on either side degrades to the hash comparison above rather
    # than inventing agreement — but it is said out loud.
    have_ident="$(running_ident)"
    if [[ -z "$have_ident" ]]; then
      log "🎯T580 the serving daemon predates source-identity stamping; comparing the binary hash only"
    elif [[ "$have_ident" != "$WANT_IDENT" ]]; then
      log "🎯T580 same binary hash, different sources (serving ${have_ident:0:12}… want ${WANT_IDENT:0:12}…); restarting"
      return 1
    fi
  fi
  [[ "$(http_code "http://127.0.0.1:${PORT}/health")" == "200" ]] || return 1
  return 0
}

if already_activated; then
  log "🎯T218 already activated: :$PORT serves this exact build (sha ${WANT_SHA:0:12}…); no restart needed"
  record_served 0
  exit 0
fi

# Serialisation happens in runlock, which this script re-execs itself under
# (🎯T392.5) — by the time we reach here the lock is held for the whole
# run and the kernel releases it even if we are killed.
#
# Re-check under the lock: whoever we queued behind has now finished, and
# they were very likely activating the same build we wanted.
if already_activated; then
  log "🎯T218 coalesced: preceding restart activated this build; no restart needed"
  record_served 0
  exit 0
fi

# The binary genuinely differs from what serves: activate it (🎯T194).
BOUNCE_WORK_START=$(date +%s)
log_t794_pre_shutdown build "pin check and rebuild while :$PORT still serves"
stop_brew_jevons
if ! upgrade_with_supervisor; then
kill_port_listeners

# 🎯T405 TEST SEAM. The window between freeing the port and starting the
# replacement is where this script died on 2026-08-10; the supervisor
# oracle has to be able to reproduce that exactly, and waiting to catch
# a real run in a sub-second window is not a test. Inert unless the
# variable is set to this exact value.
if [[ "${JEVONS_RESTART_FAULT:-}" == "after-kill" ]]; then
  log "🎯T405 fault injection: dying after freeing :$PORT and before starting the daemon"
  kill -9 $$
fi

# No supervisor owns this port (🎯T1111: supervisord handles the development
# daemon above; this is the isolate / hand-run path), so detach a daemon of
# our own.
start_daemon_detached
fi
wait_until_serving
HEALTH_READY_EPOCH=$(date +%s)
report_t794_bounce_timing "$HEALTH_READY_EPOCH"
record_active_identity

log "OK: development jevonsd serving on :$PORT (workdir=$WORKDIR)"
record_served 1
exit 0
