#!/usr/bin/env bash
# 🎯T191 — rebuild + restart the daily jevonsd on :13705.
#
# Purpose: overseer/PO/worker bounce after daemon-path Build without asking
# the owner to restart by hand (🎯T188). Session death must not cancel the
# bounce, and since 🎯T405 that no longer depends on the caller: the script
# re-execs itself into its own session unconditionally (see SELF-DETACH).
#
# Steps:
#   0. re-exec into our own session (🎯T405 SELF-DETACH below)
#   1. make / rebuild bin/jevonsd
#   2. decide whether a restart is needed at all (🎯T218 thrash policy below)
#   3. brew services stop jevons (so Cellar KeepAlive cannot reclaim :13705)
#   4. SIGHUP listeners on the daily port — 🎯T40 upgrade exit, so agent
#      turns in flight are NOT cancelled by the bounce (🎯T392.5)
#   5. start $REPO/bin/jevonsd with workdir set (detached: nohup/setsid)
#   6. wait until HTTP /health is ok and /api/frontier is not 404
#   7. exit 0 only when the new process is serving
#
# 🎯T218 THRASH POLICY — why this script often does nothing, on purpose.
#
# Many fleet workers land daemon-path changes concurrently and each calls
# this script. Naively that SIGTERMs :13705 every minute and leaves owner
# chat unusable. Observed 2026-08-05: five restarts in four minutes, every
# one of them rebuilding nothing ("make: `bin/jevonsd' is up to date") —
# a healthy daemon killed and replaced by the byte-identical binary.
#
# Three rules bound that, without ever letting a real change go stale
# (🎯T194 says a daemon change is not achieved until it actually serves):
#
#   ALREADY-ACTIVATED — if the freshly built binary hashes equal to the one
#     the running daemon was started from, and that daemon is healthy, there
#     is nothing to activate. Exit 0 without touching the port. This is a
#     true no-op, not a deferral: the daily path already serves this exact
#     build, so the caller's activation claim is honest.
#
#   ONE-AT-A-TIME — a lock dir serialises restarts. Concurrent callers wait
#     for the holder instead of racing to kill the same port, then re-run the
#     ALREADY-ACTIVATED check. N workers landing the same build coalesce into
#     a single bounce.
#
#   WAIT, DON'T SKIP — when the binary genuinely differs but the last restart
#     was under MIN_INTERVAL_SEC ago, sleep out the remainder and then restart.
#     Never silently skip a real change: a skip would report success while a
#     stale binary kept serving, which is exactly the 🎯T194 failure mode.
#
# So: identical build → free no-op; new build → always activated, at most
# one bounce per MIN_INTERVAL_SEC. --force overrides all three (owner debug).
#
# SELF-DETACH (🎯T405) — being invoked wrongly cannot cause an outage.
#
# This script kills the daily daemon, and the daemon's shutdown stops
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
#   nohup "$REPO/scripts/restart-daily-jevonsd.sh" \
#     >>"$HOME/.jevons/restart-daily.log" 2>&1 &
# Prefer nohup (portable). If setsid is available:
#   setsid "$REPO/scripts/restart-daily-jevonsd.sh" \
#     >>"$HOME/.jevons/restart-daily.log" 2>&1 < /dev/null &
#
# The daemon itself is also started under nohup/setsid so it outlives
# this script (reparented to init).
#
# SUPERVISION (🎯T405): the launchd job com.marcelocantos.jevons-watchdog
# probes :$PORT every 30s and calls this script when the port stays dead —
# so a bounce that fails for any other reason is also recovered without
# the owner. Install with `make watchdog-install`.
#
# macOS bash 3.2 safe (no mapfile/associative arrays/bash-4isms).
#
# Usage:
#   ./scripts/restart-daily-jevonsd.sh
#   ./scripts/restart-daily-jevonsd.sh --dry-run
#   ./scripts/restart-daily-jevonsd.sh -h | --help
#
# Env overrides:
#   JEVONS_RESTART_PORT     default 13705
#   JEVONS_RESTART_WORKDIR  default: repo root
#   JEVONS_RESTART_LOG      default: ~/.jevons/daily-jevonsd.log
#   JEVONS_RESTART_WAIT_SEC default 90
#   JEVONS_RESTART_STOP_WAIT_SEC  grace for the 🎯T392.5 upgrade exit to
#                               release the port before SIGKILL, default 20
#   JEVONS_RESTART_SKIP_MAKE=1  skip rebuild (use existing bin/jevonsd)
#   JEVONS_RESTART_MIN_INTERVAL_SEC  thrash window, default 180 (🎯T218)
#   JEVONS_RESTART_LOCK_WAIT_SEC     wait for an in-flight restart, default 240
#   JEVONS_RESTART_STAMP    epoch of last successful restart
#   JEVONS_RESTART_ACTIVE   "<pid> <sha256>" of the daemon we started
#   JEVONS_RESTART_BIN      daemon binary to start (default: $REPO/bin/jevonsd)
#   JEVONS_RESTART_NO_DETACH=1  skip the 🎯T405 self-detach re-exec
#   JEVONS_RESTART_DETACH_LOG   where the detached run writes (default
#                               ~/.jevons/restart-daily.log)
#   JEVONS_RESTART_FAULT=after-kill  test seam: die between freeing the
#                               port and starting the daemon (🎯T405 oracle)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DAILY_PORT=13705
PORT="${JEVONS_RESTART_PORT:-$DAILY_PORT}"
WORKDIR="${JEVONS_RESTART_WORKDIR:-$ROOT}"
LOG="${JEVONS_RESTART_LOG:-$HOME/.jevons/daily-jevonsd.log}"
WAIT_SEC="${JEVONS_RESTART_WAIT_SEC:-90}"
# 🎯T392.5: how long to let the upgrade exit (SIGHUP) release the port
# before escalating to SIGKILL. See kill_port_listeners.
STOP_WAIT_SEC="${JEVONS_RESTART_STOP_WAIT_SEC:-20}"
# 🎯T218: min seconds between successful restarts (debounce thrash from
# concurrent workers each bouncing daily after every land).
MIN_INTERVAL_SEC="${JEVONS_RESTART_MIN_INTERVAL_SEC:-180}"
STAMP_FILE="${JEVONS_RESTART_STAMP:-$HOME/.jevons/restart-daily.last}"
# 🎯T218: identity of the daemon this script last started ("<pid> <sha256>"),
# so a caller whose build is already serving can no-op instead of bouncing.
ACTIVE_FILE="${JEVONS_RESTART_ACTIVE:-$HOME/.jevons/restart-daily.active}"
# 🎯T392.5: serialise concurrent restarts under one exclusive lock.
#
# This replaces a mkdir-based mutex that raced. Between `mkdir` succeeding
# and the holder writing its pid file, a second caller read an empty pid,
# concluded the live holder was stale, rm -rf'd the lock and proceeded.
# On 2026-08-09 five restarts fired in 17 minutes, the last two 59s apart;
# the second killed the first mid-flight and left the daily daemon down.
# Every agent turn in flight was cancelled, and cancelled turns bill a full
# context for discarded work — 6.6% of the 🎯T392 baseline.
#
# flock(2) has no such window and the kernel releases it when the holder
# dies, so a crashed run cannot wedge the fleet. Locking is a Go binary
# because a mutex in shell is a race per line (shared bash doctrine).
LOCK_FILE="${JEVONS_RESTART_LOCK:-$HOME/.jevons/restart-daily.lock}"
LOCK_WAIT_SEC="${JEVONS_RESTART_LOCK_WAIT_SEC:-240}"
RUNLOCK="$ROOT/bin/runlock"
# 🎯T254.2: the daemon is compiled from a detached worktree at HEAD, not from
# the shared clone. Kept between runs so the Go build cache stays warm.
BUILDSNAP="$ROOT/bin/buildsnap"
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
DETACH_LOG="${JEVONS_RESTART_DETACH_LOG:-$HOME/.jevons/restart-daily.log}"

# --help and --dry-run touch nothing, so they neither detach nor take the
# lock: introspection that blocks behind a live restart, or that appends
# to the restart log, is a trap for anyone debugging one.
WANTS_WORK=1
for _arg in ${@+"$@"}; do
  case "$_arg" in
    -h|--help|--dry-run) WANTS_WORK=0 ;;
  esac
done

if [[ "$WANTS_WORK" == "1" && "${JEVONS_RESTART_DETACHED:-0}" != "1" && "${JEVONS_RESTART_NO_DETACH:-0}" != "1" ]]; then
  if [[ ! -x "$DETACH" ]]; then
    (cd "$ROOT" && go build -o "$DETACH" ./cmd/detach) || {
      echo "restart-daily-jevonsd: cannot build $DETACH — refusing to restart where the caller's death can cancel it" >&2
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
      echo "restart-daily-jevonsd: cannot build $RUNLOCK — refusing to restart unserialised" >&2
      exit 2
    }
  fi
  export JEVONS_RESTART_LOCKED=1
  exec "$RUNLOCK" -timeout "${LOCK_WAIT_SEC}s" "$LOCK_FILE" "$0" "$@"
fi

BIN="${JEVONS_RESTART_BIN:-$ROOT/bin/jevonsd}"
DRY_RUN=0
SKIP_MAKE="${JEVONS_RESTART_SKIP_MAKE:-0}"
FORCE=0

usage() {
  cat <<'EOF'
Usage: restart-daily-jevonsd.sh [options]

Rebuild bin/jevonsd, stop brew KeepAlive if needed, free the daily port,
start repo bin/jevonsd detached, wait until /health and /api/frontier serve.

🎯T218 thrash policy: if the rebuilt binary is already the one the running
daemon is serving, this exits 0 without touching the port (already
activated). Concurrent callers serialise on a lock and coalesce. A binary
that genuinely changed always gets activated — if the last restart was
inside the min-interval, the script waits out the remainder rather than
skipping, so a real change never reports success while a stale binary serves.

Options:
  -h, --help     Show this help and exit 0
  --dry-run      Print planned steps; do not stop/start/kill
  --force        Bypass the thrash policy: restart even if the running
                 daemon already serves this build, and do not wait out the
                 min-interval. Owner-debug escape hatch — fleet agents
                 should not use it, since an unchanged binary needs no
                 bounce and a changed one is activated without it (🎯T218).

Env:
  JEVONS_RESTART_PORT              Listen port (default 13705)
  JEVONS_RESTART_WORKDIR           -workdir for jevonsd (default: repo root)
  JEVONS_RESTART_LOG               Daemon log file (default ~/.jevons/daily-jevonsd.log)
  JEVONS_RESTART_WAIT_SEC          Health wait seconds (default 90)
  JEVONS_RESTART_STOP_WAIT_SEC     Grace for the upgrade exit to free the port
                                   before SIGKILL (default 20; 🎯T392.5)
  JEVONS_RESTART_SKIP_MAKE         If 1, skip make rebuild
  JEVONS_RESTART_MIN_INTERVAL_SEC  Thrash window (default 180; 🎯T218)
  JEVONS_RESTART_LOCK_WAIT_SEC     Wait for in-flight restart (default 240; 🎯T218)
  JEVONS_RESTART_STAMP             Stamp file for last success (default ~/.jevons/restart-daily.last)
  JEVONS_RESTART_ACTIVE            Running-daemon identity (default ~/.jevons/restart-daily.active)
  JEVONS_RESTART_LOCK              Lock dir (default ~/.jevons/restart-daily.lock)
  JEVONS_RESTART_BIN               Daemon binary (default $REPO/bin/jevonsd)
  JEVONS_RESTART_NO_DETACH         If 1, skip the 🎯T405 self-detach re-exec
  JEVONS_RESTART_DETACH_LOG        Detached run's log (default ~/.jevons/restart-daily.log)

SELF-DETACH (🎯T405): this script re-execs itself through bin/detach into
its own session before doing anything, so the caller's death — including
the agent that this restart's own kill is about to stop — cannot cancel
the bounce. A caller that is still alive still gets the exit status.

BLESSED INVOKE (survive agent/overseer death AND stop the caller blocking
on the bounce; the self-detach above covers the first half on its own):
  nohup ./scripts/restart-daily-jevonsd.sh >>"$HOME/.jevons/restart-daily.log" 2>&1 &

SUPERVISION (🎯T405): com.marcelocantos.jevons-watchdog probes the port
every 30s and calls this script when it stays dead — so a bounce that
fails for any other reason is recovered without the owner. Install it
with: make watchdog-install
EOF
}

log() {
  printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*"
}

die() {
  log "ERROR: $*"
  exit 1
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
    --force)
      FORCE=1
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

await_min_interval() {
  # A real binary change inside the thrash window waits out the remainder
  # instead of skipping: skipping would report success while the old binary
  # kept serving (🎯T194). Bounded by MIN_INTERVAL_SEC by construction.
  [[ "$FORCE" -eq 0 ]] || return 0
  [[ -f "$STAMP_FILE" ]] || return 0
  local last now elapsed remain
  last="$(cat "$STAMP_FILE" 2>/dev/null || true)"
  [[ "$last" =~ ^[0-9]+$ ]] || return 0
  now="$(date +%s)"
  elapsed=$((now - last))
  [[ "$elapsed" -ge 0 ]] || return 0
  if [[ "$elapsed" -lt "$MIN_INTERVAL_SEC" ]]; then
    remain=$((MIN_INTERVAL_SEC - elapsed))
    log "🎯T218 thrash window: last restart ${elapsed}s ago (min ${MIN_INTERVAL_SEC}s); waiting ${remain}s before activating the new binary"
    sleep "$remain"
  fi
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
  # Stop it whenever brew is present so repo bin/jevonsd owns the daily port.
  #
  # Only for the daily port: the Cellar service is configured for :13705 and
  # can never be holding a throwaway one, so stopping it on behalf of a test
  # bounce would take the owner's daemon down to fix a port it does not have
  # (🎯T405 — the oracle drives this script on a scratch port).
  if [[ "$PORT" != "$DAILY_PORT" ]]; then
    log "port :$PORT is not the daily :$DAILY_PORT; leaving brew services alone"
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
  local sha listeners
  sha="$(binary_sha "$BIN" || true)"
  listeners="$(list_listen_pids | tr -d '[:space:]')"
  if [[ -n "$sha" && -n "$listeners" ]]; then
    mkdir -p "$(dirname "$ACTIVE_FILE")" 2>/dev/null || true
    printf '%s %s\n' "$listeners" "$sha" >"$ACTIVE_FILE" 2>/dev/null || true
    log "🎯T218 active: pid=$listeners sha=${sha:0:12}…"
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
  die "timed out after ${deadline}s waiting for daily jevonsd on :$PORT (log=$LOG)"
}

# --- main --------------------------------------------------------------------

log "🎯T191 restart-daily-jevonsd: root=$ROOT port=$PORT workdir=$WORKDIR dry_run=$DRY_RUN force=$FORCE"

if [[ "$DRY_RUN" -eq 1 ]]; then
  log "[dry-run] would: 🎯T254.2 build bin/jevonsd from committed HEAD in $SNAP_DIR (never the shared tree) unless SKIP_MAKE=$SKIP_MAKE"
  log "[dry-run] would: 🎯T218 no-op if the running daemon already serves this build (already activated)"
  log "[dry-run] would: 🎯T405 re-exec through $DETACH into its own session so the caller's death cannot cancel the bounce"
  log "[dry-run] would: 🎯T392.5 hold $LOCK_FILE via runlock so concurrent restarts serialise"
  log "[dry-run] would: 🎯T218 wait out the ${MIN_INTERVAL_SEC}s thrash window rather than skip a changed binary"
  log "[dry-run] would: brew services stop jevons (if brew lists jevons)"
  log "[dry-run] would: 🎯T392.5 SIGHUP listeners on :$PORT (upgrade exit — in-flight agent turns survive), SIGKILL only if the port is still held after ${STOP_WAIT_SEC}s"
  log "[dry-run] would: nohup/setsid start $BIN -port $PORT -workdir $WORKDIR >>$LOG"
  log "[dry-run] would: wait for /health 200 and /api/frontier non-404"
  log "[dry-run] BLESSED INVOKE: nohup $ROOT/scripts/restart-daily-jevonsd.sh >>\$HOME/.jevons/restart-daily.log 2>&1 &"
  exit 0
fi

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

  # 🎯T405: the supervisor is part of the deployment, not something a
  # human installed once. bin/jevons-watchdog was built by hand on
  # 2026-08-10 and every bounce since rebuilt the daemon around it,
  # leaving the one process responsible for the daemon being up as the
  # one process that never received a fix. launchd re-execs the program
  # each interval, and install() lands it by rename, so replacing the
  # file here is picked up on the next probe with no reload.
  #
  # Not fatal, unlike the daemon build above: a watchdog that will not
  # compile is a reason to bring the daemon up unsupervised and say so,
  # never a reason to leave it down.
  log "rebuild: bin/jevons-watchdog from committed HEAD"
  "$BUILDSNAP" -root "$ROOT" -snap "$SNAP_DIR" \
    -target bin/jevons-watchdog -artifact bin/jevons-watchdog \
    -dest "$ROOT/bin/jevons-watchdog" ||
    log "WARNING: the watchdog did not rebuild — the daemon is coming up supervised by whatever build is on disk"
else
  log "skip make (JEVONS_RESTART_SKIP_MAKE=1)"
  [[ -x "$BIN" ]] || die "no binary at $BIN"
fi

# 🎯T218: the build is now current, so we can ask the only question that
# matters — is this binary already serving? Answering it *before* killing
# anything is what turns five bounces into one.
WANT_SHA="$(binary_sha "$BIN" || true)"

already_activated() {
  # True when the daemon holding :$PORT runs WANT_SHA and answers /health.
  [[ "$FORCE" -eq 0 ]] || return 1
  [[ -n "$WANT_SHA" ]] || return 1
  local have
  have="$(running_sha)"
  [[ -n "$have" && "$have" == "$WANT_SHA" ]] || return 1
  [[ "$(http_code "http://127.0.0.1:${PORT}/health")" == "200" ]] || return 1
  return 0
}

if already_activated; then
  log "🎯T218 already activated: :$PORT serves this exact build (sha ${WANT_SHA:0:12}…); no restart needed"
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
  exit 0
fi

# The binary genuinely differs from what serves. It must be activated
# (🎯T194) — rate-limit by waiting, never by skipping.
await_min_interval

stop_brew_jevons
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

start_daemon_detached
wait_until_serving
record_active_identity

log "OK: daily jevonsd serving on :$PORT (workdir=$WORKDIR)"
# 🎯T218: stamp successful restart to open the next thrash window.
mkdir -p "$(dirname "$STAMP_FILE")" 2>/dev/null || true
date +%s >"$STAMP_FILE" 2>/dev/null || true
exit 0
