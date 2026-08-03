#!/usr/bin/env bash
# 🎯T191 — rebuild + restart the daily jevonsd on :13705.
#
# Purpose: overseer/PO/worker bounce after daemon-path Build without asking
# the owner to restart by hand (🎯T188). Session death must not cancel the
# bounce — invoke this script *detached* (see BLESSED INVOKE below).
#
# Steps:
#   1. make / rebuild bin/jevonsd
#   2. brew services stop jevons (so Cellar KeepAlive cannot reclaim :13705)
#   3. kill listeners on the daily port
#   4. start $REPO/bin/jevonsd with workdir set (detached: nohup/setsid)
#   5. wait until HTTP /health is ok and /api/frontier is not 404
#   6. exit 0 only when the new process is serving
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
BIN="$ROOT/bin/jevonsd"
DRY_RUN=0
SKIP_MAKE="${JEVONS_RESTART_SKIP_MAKE:-0}"
FORCE=0

usage() {
  cat <<'EOF'
Usage: restart-daily-jevonsd.sh [options]

Rebuild bin/jevonsd, stop brew KeepAlive if needed, free the daily port,
start repo bin/jevonsd detached, wait until /health and /api/frontier serve.

Options:
  -h, --help     Show this help and exit 0
  --dry-run      Print planned steps; do not stop/start/kill
  --force        Ignore min-interval debounce (🎯T218)

Env:
  JEVONS_RESTART_PORT              Listen port (default 13705)
  JEVONS_RESTART_WORKDIR           -workdir for jevonsd (default: repo root)
  JEVONS_RESTART_LOG               Daemon log file (default ~/.jevons/daily-jevonsd.log)
  JEVONS_RESTART_WAIT_SEC          Health wait seconds (default 90)
  JEVONS_RESTART_SKIP_MAKE         If 1, skip make rebuild
  JEVONS_RESTART_MIN_INTERVAL_SEC  Debounce window (default 180; 🎯T218)
  JEVONS_RESTART_STAMP             Stamp file for last success (default ~/.jevons/restart-daily.last)

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

# 🎯T218: refuse thrash restarts unless --force or interval elapsed.
if [[ "$FORCE" -eq 0 && "$DRY_RUN" -eq 0 && -f "$STAMP_FILE" ]]; then
  last="$(cat "$STAMP_FILE" 2>/dev/null || true)"
  now="$(date +%s)"
  if [[ "$last" =~ ^[0-9]+$ ]]; then
    elapsed=$((now - last))
    if [[ "$elapsed" -lt "$MIN_INTERVAL_SEC" ]]; then
      remain=$((MIN_INTERVAL_SEC - elapsed))
      log "🎯T218 debounce: last restart ${elapsed}s ago (min ${MIN_INTERVAL_SEC}s); skip (remain ${remain}s). Use --force to override."
      exit 0
    fi
  fi
fi

# --- helpers -----------------------------------------------------------------

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

log "🎯T191 restart-daily-jevonsd: root=$ROOT port=$PORT workdir=$WORKDIR dry_run=$DRY_RUN"

if [[ "$DRY_RUN" -eq 1 ]]; then
  log "[dry-run] would: make (bin/jevonsd) unless SKIP_MAKE=$SKIP_MAKE"
  log "[dry-run] would: brew services stop jevons (if brew lists jevons)"
  log "[dry-run] would: kill listeners on :$PORT"
  log "[dry-run] would: nohup/setsid start $BIN -port $PORT -workdir $WORKDIR >>$LOG"
  log "[dry-run] would: wait for /health 200 and /api/frontier non-404"
  log "[dry-run] BLESSED INVOKE: nohup $ROOT/scripts/restart-daily-jevonsd.sh >>\$HOME/.jevons/restart-daily.log 2>&1 &"
  exit 0
fi

if [[ "$SKIP_MAKE" != "1" ]]; then
  log "rebuild: make bin/jevonsd"
  make -C "$ROOT" bin/jevonsd
else
  log "skip make (JEVONS_RESTART_SKIP_MAKE=1)"
  [[ -x "$BIN" ]] || die "no binary at $BIN"
fi

stop_brew_jevons
kill_port_listeners
start_daemon_detached
wait_until_serving

log "OK: daily jevonsd serving on :$PORT (workdir=$WORKDIR)"
# 🎯T218: stamp successful restart for debounce window.
mkdir -p "$(dirname "$STAMP_FILE")" 2>/dev/null || true
date +%s >"$STAMP_FILE" 2>/dev/null || true
exit 0
