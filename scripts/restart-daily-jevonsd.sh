#!/usr/bin/env bash
# 🎯T191 — rebuild + restart the daily jevonsd on :13705.
#
# Purpose: overseer/PO/worker bounce after daemon-path Build without asking
# the owner to restart by hand (🎯T188). Session death must not cancel the
# bounce — invoke this script *detached* (see BLESSED INVOKE below).
#
# Steps:
#   1. make / rebuild bin/jevonsd
#   2. decide whether a restart is needed at all (🎯T218 thrash policy below)
#   3. brew services stop jevons (so Cellar KeepAlive cannot reclaim :13705)
#   4. kill listeners on the daily port
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
# BLESSED INVOKE (fleet agent / overseer — survive parent death):
#   nohup "$REPO/scripts/restart-daily-jevonsd.sh" \
#     >>"$HOME/.jevons/restart-daily.log" 2>&1 &
# Prefer nohup (portable). If setsid is available:
#   setsid "$REPO/scripts/restart-daily-jevonsd.sh" \
#     >>"$HOME/.jevons/restart-daily.log" 2>&1 < /dev/null &
#
# NEVER run this script as a foreground child of a fleet agent without
# detach. Agent SIGHUP/SIGTERM will kill the bounce mid-flight and leave
# the owner on a dead or stale binary. The daemon itself is also started
# under nohup/setsid so it outlives this script (reparented to init).
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
#   JEVONS_RESTART_SKIP_MAKE=1  skip rebuild (use existing bin/jevonsd)
#   JEVONS_RESTART_MIN_INTERVAL_SEC  thrash window, default 180 (🎯T218)
#   JEVONS_RESTART_LOCK_WAIT_SEC     wait for an in-flight restart, default 240
#   JEVONS_RESTART_STAMP    epoch of last successful restart
#   JEVONS_RESTART_ACTIVE   "<pid> <sha256>" of the daemon we started

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PORT="${JEVONS_RESTART_PORT:-13705}"
WORKDIR="${JEVONS_RESTART_WORKDIR:-$ROOT}"
LOG="${JEVONS_RESTART_LOG:-$HOME/.jevons/daily-jevonsd.log}"
WAIT_SEC="${JEVONS_RESTART_WAIT_SEC:-90}"
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

# Re-exec under the lock unless we are already the locked child. Fails
# closed: restarting unserialised is the failure mode this exists to stop,
# so a missing runlock is built, and an unbuildable one aborts.
if [[ "${JEVONS_RESTART_LOCKED:-0}" != "1" && "${JEVONS_RESTART_NO_LOCK:-0}" != "1" ]]; then
  if [[ ! -x "$RUNLOCK" ]]; then
    (cd "$ROOT" && go build -o "$RUNLOCK" ./cmd/runlock) || {
      echo "restart-daily-jevonsd: cannot build $RUNLOCK — refusing to restart unserialised" >&2
      exit 2
    }
  fi
  export JEVONS_RESTART_LOCKED=1
  exec "$RUNLOCK" -timeout "${LOCK_WAIT_SEC}s" "$LOCK_FILE" "$0" "$@"
fi

BIN="$ROOT/bin/jevonsd"
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
  JEVONS_RESTART_SKIP_MAKE         If 1, skip make rebuild
  JEVONS_RESTART_MIN_INTERVAL_SEC  Thrash window (default 180; 🎯T218)
  JEVONS_RESTART_LOCK_WAIT_SEC     Wait for in-flight restart (default 240; 🎯T218)
  JEVONS_RESTART_STAMP             Stamp file for last success (default ~/.jevons/restart-daily.last)
  JEVONS_RESTART_ACTIVE            Running-daemon identity (default ~/.jevons/restart-daily.active)
  JEVONS_RESTART_LOCK              Lock dir (default ~/.jevons/restart-daily.lock)

BLESSED INVOKE (survive agent/overseer death):
  nohup ./scripts/restart-daily-jevonsd.sh >>"$HOME/.jevons/restart-daily.log" 2>&1 &

Never run as a foreground child of a fleet agent without detach (🎯T191).
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
  local pids
  pids="$(list_listen_pids)"
  if [[ -z "$pids" ]]; then
    log "no listeners on :$PORT"
    return 0
  fi
  log "killing listeners on :$PORT: $(echo "$pids" | tr '\n' ' ')"
  # Word-split is intentional: kill accepts multiple PIDs.
  # shellcheck disable=SC2086
  kill $pids 2>/dev/null || true
  sleep 1
  pids="$(list_listen_pids)"
  if [[ -n "$pids" ]]; then
    log "force-killing remaining on :$PORT: $(echo "$pids" | tr '\n' ' ')"
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
  log "[dry-run] would: 🎯T392.5 hold $LOCK_FILE via runlock so concurrent restarts serialise"
  log "[dry-run] would: 🎯T218 wait out the ${MIN_INTERVAL_SEC}s thrash window rather than skip a changed binary"
  log "[dry-run] would: brew services stop jevons (if brew lists jevons)"
  log "[dry-run] would: kill listeners on :$PORT"
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
start_daemon_detached
wait_until_serving
record_active_identity

log "OK: daily jevonsd serving on :$PORT (workdir=$WORKDIR)"
# 🎯T218: stamp successful restart to open the next thrash window.
mkdir -p "$(dirname "$STAMP_FILE")" 2>/dev/null || true
date +%s >"$STAMP_FILE" 2>/dev/null || true
exit 0
