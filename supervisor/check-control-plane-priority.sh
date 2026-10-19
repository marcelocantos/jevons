#!/bin/sh
# Live oracle for 🎯T779: control-plane daemons are not below fleet-shell PRI.
#
# Samples `ps` for supervisord + known control-plane binaries, compares the
# minimum observed PRI against a fleet-shell baseline (zsh/bash children that
# look like agent shells, falling back to 31 — the measured Interactive floor
# for Aqua terminals on this host).
#
# Exit 0 = control plane min PRI >= baseline.
# Exit 1 = starved (control plane below baseline) or no control-plane rows.
# Exit 2 = usage / environment error.
#
# Pure classification of already-sampled rows lives in internal/hostprio so
# hermetic tests do not need a live supervisord.
set -e

BASELINE_DEFAULT=31
BASELINE="${FLEET_PRI_BASELINE:-}"
PS_FILE="${HOSTPRIO_PS_FILE:-}"
JSON=0

usage() {
  cat <<'EOF'
usage: check-control-plane-priority.sh [--json]

  Compare live (or fixture) process PRI for control-plane daemons against
  fleet-shell baseline. 🎯T779 oracle.

Env:
  FLEET_PRI_BASELINE   force baseline PRI (default: measured shells or 31)
  HOSTPRIO_PS_FILE     read `ps -axo pid,ppid,pri,ni,command` lines from a file
                       instead of the live system (tests)
EOF
}

for arg in "$@"; do
  case "$arg" in
    --json) JSON=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -n "$PS_FILE" ]; then
  if [ ! -f "$PS_FILE" ]; then
    echo "HOSTPRIO_PS_FILE not found: $PS_FILE" >&2
    exit 2
  fi
  PS_OUT="$(cat "$PS_FILE")"
else
  PS_OUT="$(ps -axo pid=,ppid=,pri=,ni=,command=)"
fi

# Emit TSV: kind pri pid command  where kind is control|fleet|other
classified="$(
  printf '%s\n' "$PS_OUT" | awk '
  function is_control(cmd) {
    if (cmd ~ /supervisord/) return 1
    if (cmd ~ /\/jevonsd( |$)/ || cmd ~ /bin\/jevonsd( |$)/) return 1
    if (cmd ~ /claudia broker/ || cmd ~ /\/claudia broker/) return 1
    if (cmd ~ /\/bullseye serve/ || cmd ~ /bin\/bullseye serve/) return 1
    if (cmd ~ /\/mnemo( |$)/ || cmd ~ /bin\/mnemo( |$)/) return 1
    if (cmd ~ /run-jevonsd\.sh|run-bullseye\.sh|run-mnemo\.sh|run-claudia\.sh/) return 1
    return 0
  }
  function is_fleet(cmd) {
    # Agent shells and common seat entrypoints — not GUI helpers.
    if (cmd ~ /(^|\/)(zsh|bash) / && cmd ~ /(claude|codex|cursor-agent|agent_start|jevons-broker|mcp-server)/) return 1
    if (cmd ~ /claude-code|claude\.app\/Contents\/MacOS\/claude/) return 1
    if (cmd ~ /\/bin\/zsh -c / || cmd ~ /\/bin\/bash -lc /) return 1
    return 0
  }
  {
    pid=$1; ppid=$2; pri=$3; ni=$4
    $1=$2=$3=$4=""
    sub(/^ +/, "", $0)
    cmd=$0
    if (pid == "" || pri+0 != pri) next
    if (is_control(cmd)) { print "control", pri+0, pid, cmd; next }
    if (is_fleet(cmd))   { print "fleet",   pri+0, pid, cmd; next }
  }
  '
)"

ctrl_min=""
ctrl_n=0
fleet_max=""
fleet_n=0

while IFS= read -r line; do
  [ -z "$line" ] && continue
  kind="$(printf '%s\n' "$line" | awk '{print $1}')"
  pri="$(printf '%s\n' "$line" | awk '{print $2}')"
  case "$kind" in
    control)
      ctrl_n=$((ctrl_n + 1))
      if [ -z "$ctrl_min" ] || [ "$pri" -lt "$ctrl_min" ]; then
        ctrl_min="$pri"
      fi
      ;;
    fleet)
      fleet_n=$((fleet_n + 1))
      if [ -z "$fleet_max" ] || [ "$pri" -gt "$fleet_max" ]; then
        fleet_max="$pri"
      fi
      ;;
  esac
done <<EOF
$classified
EOF

if [ -z "$BASELINE" ]; then
  if [ -n "$fleet_max" ] && [ "$fleet_max" -gt 0 ]; then
    BASELINE="$fleet_max"
  else
    BASELINE="$BASELINE_DEFAULT"
  fi
fi

if [ "$ctrl_n" -eq 0 ] || [ -z "$ctrl_min" ]; then
  if [ "$JSON" = 1 ]; then
    printf '{"ok":false,"reason":"no_control_plane_rows","baseline":%s,"control_min":null,"control_n":0,"fleet_n":%s}\n' \
      "$BASELINE" "$fleet_n"
  else
    echo "FAIL 🎯T779: no control-plane process rows found" >&2
  fi
  exit 1
fi

ok=0
if [ "$ctrl_min" -ge "$BASELINE" ]; then
  ok=1
fi

if [ "$JSON" = 1 ]; then
  if [ "$ok" = 1 ]; then
    printf '{"ok":true,"baseline":%s,"control_min":%s,"control_n":%s,"fleet_n":%s,"fleet_max":%s}\n' \
      "$BASELINE" "$ctrl_min" "$ctrl_n" "$fleet_n" "${fleet_max:-null}"
  else
    printf '{"ok":false,"reason":"starved","baseline":%s,"control_min":%s,"control_n":%s,"fleet_n":%s,"fleet_max":%s}\n' \
      "$BASELINE" "$ctrl_min" "$ctrl_n" "$fleet_n" "${fleet_max:-null}"
  fi
else
  echo "control_plane min_pri=$ctrl_min n=$ctrl_n"
  echo "fleet_shells max_pri=${fleet_max:-n/a} n=$fleet_n baseline=$BASELINE"
  if [ "$ok" = 1 ]; then
    echo "PASS 🎯T779: control plane PRI $ctrl_min >= baseline $BASELINE"
  else
    echo "FAIL 🎯T779: control plane PRI $ctrl_min < baseline $BASELINE (starved)" >&2
    echo "fix: supervisor/ensure-interactive-priority.sh && supervisor/ensure-interactive-priority.sh --apply" >&2
  fi
fi

[ "$ok" = 1 ]
