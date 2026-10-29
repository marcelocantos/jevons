#!/bin/sh
# Render supervisor/*.ini into supervisor.d (vellum supervisor/install.sh)
# and make supervisord the owner of the development daemon's lifecycle (🎯T594).
#
# Exactly one thing may own :13705. This installer therefore evicts the
# other claimant before starting its own: Homebrew's brew-services copy.
# Leaving it loaded is how the program ends up FATAL "exited too quickly" —
# supervisord starts a second daemon, the bind fails, it exits inside
# startsecs, and after startretries supervisord gives up FOR GOOD. That
# state looks like a warning and is actually an unsupervised daemon
# (observed 2026-08-29 to 08-31). launchd is not a claimant any more:
# 🎯T1111 retired the KeepAlive job and the UI-probe LaunchAgents.
#
# SUPERVISOR_NO_TAKEOVER=1 renders the programs without taking the port,
# for a machine where something else still holds it.
# SUPERVISOR_SKIP_LAUNCHAGENT=1 skips the 🎯T779 Interactive LaunchAgent
# install (hermetic tests and render-only runs).
set -e

REPO="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
CONF_DIR="${SUPERVISOR_CONF_DIR:-/opt/homebrew/etc/supervisor.d}"

# 🎯T1184: the production configs always name the shared clone. Run from a
# linked worktree, this installer rendered jevonsd.ini around the worker's
# tree (2026-10-22 15:46, jv-t779-auto), and every development restart then
# launched that worker's build. Hand the production render to the main
# working tree's installer; a test SUPERVISOR_CONF_DIR renders locally.
if [ -z "${SUPERVISOR_CONF_DIR:-}" ]; then
  git_dir="$(git -C "$REPO" rev-parse --path-format=absolute --git-dir 2>/dev/null || true)"
  common_dir="$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  if [ -n "$git_dir" ] && [ -n "$common_dir" ] && [ "$git_dir" != "$common_dir" ]; then
    main_root="$(dirname "$common_dir")"
    echo "supervisor/install.sh: invoked from linked worktree $REPO; rendering production configs from the shared clone $main_root (🎯T1184)" >&2
    exec "$main_root/supervisor/install.sh" "$@"
  fi
fi

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi

# 🎯T1103 — same host-sentinel pause contract as scripts/restart-jevonsd.sh
# (docs/design/sentinel-pause-contract.md). supervisord autostart reaches
# the port without going through that script, so this installer checks the
# marker itself before rendering or (re)starting anything.
PAUSE_MARKER="${JEVONS_RESTART_PAUSE_MARKER:-$HOME/.local/share/sen/jevons-paused.txt}"
PAUSE_OVERRIDE="${JEVONS_RESTART_OVERRIDE_PAUSE:-0}"
if [ -f "$PAUSE_MARKER" ] && [ "$PAUSE_OVERRIDE" != "1" ]; then
  echo "supervisor/install.sh: refusing - host-sentinel pause marker present at $PAUSE_MARKER:" >&2
  sed 's/^/  /' "$PAUSE_MARKER" >&2
  echo "" >&2
  echo "This script will not race the sentinel. Lift the pause through the" >&2
  echo "sentinel's own unpause step (docs/design/sentinel-pause-contract.md);" >&2
  echo "do not delete the marker by hand from here." >&2
  echo "To proceed anyway on the owner's explicit word:" >&2
  echo "  JEVONS_RESTART_OVERRIDE_PAUSE=1 $0 \"\$@\"" >&2
  exit 3
fi

mkdir -p "$CONF_DIR"
mkdir -p "$HOME/.local/var/log"
chmod +x "$REPO/supervisor/run-jevonsd.sh"

render() {
  name="$1"
  dest="$CONF_DIR/${name}.ini"
  template="$REPO/supervisor/${name}.ini"
  rm -f "$dest"
  sed "s|@REPO@|$REPO|g" "$template" >"$dest"
  echo "rendered $dest (from $template)"
}

if [ "${SUPERVISOR_RETIRE_VANILLA_ONLY:-}" != 1 ]; then
  if [ "${SUPERVISOR_SKIP_CTL:-}" != 1 ] && [ "${SUPERVISOR_NO_TAKEOVER:-}" != 1 ] && ! command -v claudia >/dev/null 2>&1; then
    echo "claudia is required before replacing jevons-broker" >&2
    exit 1
  fi
  render jevonsd
  # Phone access (jevons.canticode.com, owner-only Google sign-in at the edge).
  # Rendered only where the edge policy exists: without it the tunnel would
  # either refuse to start or, worse, be configured by hand without it.
  if [ -f "$HOME/.config/ngrok/jevons-policy.yml" ]; then
    render jevons-tunnel
  fi
  # Claudia owns the host broker socket. Retire the Jevons broker
  # definition so a later supervisord reload cannot reclaim it.
  rm -f "$CONF_DIR/jevons-broker.ini"
fi
# Retire the comparison program. supervisorctl update removes the old group;
# there is no template left that can recreate it on the next installation.
rm -f "$CONF_DIR/jevons-vanilla.ini"

if [ "${SUPERVISOR_SKIP_CTL:-}" = 1 ]; then
  exit 0
fi

if ! command -v supervisorctl >/dev/null 2>&1; then
  echo "supervisorctl not on PATH — ini written; start Homebrew supervisor to load it" >&2
  exit 1
fi

changes="$(supervisorctl reread)"
printf '%s\n' "$changes"
case "$changes" in
  *"jevons-vanilla: disappeared"*) supervisorctl update jevons-vanilla ;;
esac

# A retirement-only activation never updates or restarts the main program,
# even if reread notices unrelated pending configuration changes.
if [ "${SUPERVISOR_RETIRE_VANILLA_ONLY:-}" = 1 ]; then
  echo "vanilla comparison service retired; development daemon left untouched"
  exit 0
fi

if [ "${SUPERVISOR_NO_TAKEOVER:-}" = 1 ]; then
  # Rendered but not started. autostart=true means supervisord will still
  # try on its next reload, so say plainly that the port must be free by
  # then or the program lands in FATAL.
  echo "jevonsd: rendered, not started (SUPERVISOR_NO_TAKEOVER=1)."
  echo "jevonsd: free :13705 before supervisord reloads, or the program goes FATAL."
else
  supervisorctl stop jevons-broker 2>/dev/null || true
  supervisorctl update jevons-broker 2>/dev/null || true
  # Both brokers speak the same status protocol, so a successful status
  # alone cannot prove the old process released the socket.
  if command -v pgrep >/dev/null 2>&1 && pgrep -f '[j]evons-broker serve' >/dev/null 2>&1; then
    echo "jevons-broker is still running; refusing to restart jevonsd against the old owner" >&2
    exit 1
  fi
  if [ -f "$CONF_DIR/claudia.ini" ]; then
    supervisorctl update claudia
  fi
  if ! claudia broker status >/dev/null 2>&1; then
    if [ -f "$CONF_DIR/claudia.ini" ]; then
      supervisorctl start claudia 2>/dev/null || supervisorctl restart claudia
    elif command -v brew >/dev/null 2>&1; then
      brew services start claudia
    else
      echo "claudia broker is down and no supervisor or Homebrew service is available" >&2
      exit 1
    fi
  fi
  i=0
  while [ "$i" -lt 20 ] && ! claudia broker status >/dev/null 2>&1; do
    sleep 0.5
    i=$((i + 1))
  done
  if ! claudia broker status >/dev/null 2>&1; then
    echo "claudia broker did not become ready; leaving jevonsd untouched" >&2
    exit 1
  fi
  # Stop the Cellar service too. brew services would otherwise reclaim
  # :13705 on the next boot and win the race against supervisord.
  if command -v brew >/dev/null 2>&1; then
    brew services stop jevons >/dev/null 2>&1 || true
  fi
  # Anything still holding the port is a leftover detached daemon; without
  # this the first start always loses the bind.
  if command -v lsof >/dev/null 2>&1; then
    holder="$(lsof -nP -iTCP:13705 -sTCP:LISTEN -t 2>/dev/null | head -1 || true)"
    if [ -n "$holder" ]; then
      echo "jevonsd: stopping pid $holder still holding :13705"
      kill "$holder" 2>/dev/null || true
      i=0
      while [ $i -lt 20 ] && kill -0 "$holder" 2>/dev/null; do
        sleep 0.25
        i=$((i + 1))
      done
      kill -9 "$holder" 2>/dev/null || true
    fi
  fi
  # reread only discovers definitions; update registers a fresh group or
  # replaces its loaded configuration. Scope this to the primary daemon.
  supervisorctl update jevonsd
  supervisorctl restart jevonsd 2>/dev/null || supervisorctl start jevonsd
fi

supervisorctl status jevonsd || true
claudia broker status

if [ "${SUPERVISOR_SKIP_LAUNCHAGENT:-}" != "1" ]; then
  "$REPO/supervisor/install-launchagent.sh"
fi
