#!/bin/sh
# Ensure homebrew.mxcl.supervisor runs as ProcessType=Interactive (🎯T779).
#
# Without this, launchd classifies supervisord (and every child: jevonsd,
# claudia, bullseye, mnemo, …) as a lightly throttled Background/Standard
# job — PRI 20 vs PRI 31 for fleet shells — and the control plane starves
# under load. See docs/design/control-plane-priority.md.
#
# Default: write the plist key only (survives until the next job start).
# --apply: bootout + bootstrap (or kickstart -k) so the class takes effect
# now. That restarts every supervised service — opt-in on purpose.
#
# --check: exit 0 iff the on-disk plist already has ProcessType=Interactive.
# --dry-run: print the planned write without touching the file.
set -e

LABEL="${SUPERVISOR_LAUNCH_LABEL:-homebrew.mxcl.supervisor}"
PLIST_DEFAULT=""
if [ -n "${HOME:-}" ]; then
  PLIST_DEFAULT="$HOME/Library/LaunchAgents/${LABEL}.plist"
fi
PLIST="${SUPERVISOR_LAUNCH_PLIST:-$PLIST_DEFAULT}"
APPLY=0
CHECK=0
DRY=0

usage() {
  cat <<'EOF'
usage: ensure-interactive-priority.sh [--check|--dry-run|--apply]

  (default)  Ensure ProcessType=Interactive on the supervisor LaunchAgent plist.
  --check    Exit 0 if the plist already has ProcessType=Interactive; else 1.
  --dry-run  Show whether a write would happen; do not modify.
  --apply    After ensuring the key, bounce the launchd job so PRI takes effect.
             Restarts supervisord and every supervised service.

Env:
  SUPERVISOR_LAUNCH_LABEL  default homebrew.mxcl.supervisor
  SUPERVISOR_LAUNCH_PLIST  absolute path to the plist (tests / non-default homes)
EOF
}

for arg in "$@"; do
  case "$arg" in
    --apply) APPLY=1 ;;
    --check) CHECK=1 ;;
    --dry-run) DRY=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$PLIST" ]; then
  echo "HOME unset and SUPERVISOR_LAUNCH_PLIST not set" >&2
  exit 2
fi

if [ ! -f "$PLIST" ]; then
  echo "supervisor LaunchAgent plist not found: $PLIST" >&2
  echo "start Homebrew supervisor once (brew services start supervisor) then re-run" >&2
  exit 1
fi

current=""
if command -v /usr/libexec/PlistBuddy >/dev/null 2>&1; then
  current="$(/usr/libexec/PlistBuddy -c 'Print :ProcessType' "$PLIST" 2>/dev/null || true)"
elif command -v plutil >/dev/null 2>&1; then
  current="$(plutil -extract ProcessType raw "$PLIST" 2>/dev/null || true)"
else
  current="$(grep -A1 '<key>ProcessType</key>' "$PLIST" 2>/dev/null | tail -1 | sed -n 's/.*<string>\([^<]*\)<\/string>.*/\1/p' || true)"
fi

if [ "$current" = "Interactive" ]; then
  echo "ok: $PLIST already ProcessType=Interactive"
  if [ "$CHECK" = 1 ]; then
    exit 0
  fi
  if [ "$APPLY" != 1 ]; then
    exit 0
  fi
else
  if [ "$CHECK" = 1 ]; then
    echo "missing: $PLIST ProcessType='${current:-<unset>}' (want Interactive)" >&2
    exit 1
  fi
  if [ "$DRY" = 1 ]; then
    echo "dry-run: would set ProcessType=Interactive on $PLIST (was '${current:-<unset>}')"
    exit 0
  fi
  if command -v /usr/libexec/PlistBuddy >/dev/null 2>&1; then
    if [ -n "$current" ]; then
      /usr/libexec/PlistBuddy -c 'Set :ProcessType Interactive' "$PLIST"
    else
      /usr/libexec/PlistBuddy -c 'Add :ProcessType string Interactive' "$PLIST"
    fi
  else
    echo "PlistBuddy required to write ProcessType" >&2
    exit 1
  fi
  echo "wrote: ProcessType=Interactive on $PLIST (was '${current:-<unset>}')"
fi

if [ "$APPLY" != 1 ]; then
  echo "note: launchd applies ProcessType at job start; re-run with --apply (or restart supervisor) to raise live PRI"
  exit 0
fi

if ! command -v launchctl >/dev/null 2>&1; then
  echo "launchctl not available; plist written, bounce manually" >&2
  exit 1
fi

uid="$(id -u)"
domain="gui/$uid"
target="$domain/$LABEL"

# Prefer kickstart -k when the job is already loaded (keeps the same definition
# path). Fall back to bootout+bootstrap when print says it is absent.
if launchctl print "$target" >/dev/null 2>&1; then
  echo "applying: launchctl kickstart -k $target"
  launchctl kickstart -k "$target"
else
  echo "applying: launchctl bootstrap $domain $PLIST"
  launchctl bootstrap "$domain" "$PLIST"
fi

# Wait until supervisord is back and at least one control-plane child exists.
i=0
while [ "$i" -lt 60 ]; do
  if pgrep -u "$uid" -f '[s]upervisord.*supervisord.conf' >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
  i=$((i + 1))
done
if ! pgrep -u "$uid" -f '[s]upervisord.*supervisord.conf' >/dev/null 2>&1; then
  echo "supervisord did not return after apply" >&2
  exit 1
fi
echo "applied: $target bounced; verify with supervisor/check-control-plane-priority.sh"
