#!/bin/sh
# Render supervisor/*.ini into supervisor.d (vellum supervisor/install.sh)
# and make supervisord the owner of the daily daemon's lifecycle (🎯T594).
#
# Exactly one thing may own :13705. This installer therefore evicts the
# other two claimants before starting its own: the launchd KeepAlive job,
# and Homebrew's brew-services copy. Leaving either loaded is how the
# program ends up FATAL "exited too quickly" — supervisord starts a second
# daemon, the bind fails, it exits inside startsecs, and after startretries
# supervisord gives up FOR GOOD. That state looks like a warning and is
# actually an unsupervised daemon (observed 2026-08-29 to 08-31).
#
# SUPERVISOR_NO_TAKEOVER=1 renders the programs without taking the port,
# for a machine that still wants launchd to own it.
set -e

REPO="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
CONF_DIR="${SUPERVISOR_CONF_DIR:-/opt/homebrew/etc/supervisor.d}"

if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
mkdir -p "$CONF_DIR"
mkdir -p "$HOME/.local/var/log"
chmod +x "$REPO/supervisor/run-jevonsd.sh" "$REPO/supervisor/run-vanilla.sh"

render() {
  name="$1"
  dest="$CONF_DIR/${name}.ini"
  template="$REPO/supervisor/${name}.ini"
  rm -f "$dest"
  sed "s|@REPO@|$REPO|g" "$template" >"$dest"
  echo "rendered $dest (from $template)"
}

render jevonsd
render jevons-vanilla

if [ "${SUPERVISOR_SKIP_CTL:-}" = 1 ]; then
  exit 0
fi

if ! command -v supervisorctl >/dev/null 2>&1; then
  echo "supervisorctl not on PATH — ini written; start Homebrew supervisor to load it" >&2
  exit 1
fi

supervisorctl reread
supervisorctl update

# Vanilla :13706 — bootout the LaunchAgent so binds do not fight.
if command -v launchctl >/dev/null 2>&1; then
  launchctl bootout "gui/$(id -u)/com.marcelocantos.jevons-ui-vanilla" 2>/dev/null || true
fi
supervisorctl restart jevons-vanilla 2>/dev/null || supervisorctl start jevons-vanilla

if [ "${SUPERVISOR_NO_TAKEOVER:-}" = 1 ]; then
  # Rendered but not started. autostart=true means supervisord will still
  # try on its next reload, so say plainly that the port must be free by
  # then or the program lands in FATAL.
  echo "jevonsd: rendered, not started (SUPERVISOR_NO_TAKEOVER=1)."
  echo "jevonsd: free :13705 before supervisord reloads, or the program goes FATAL."
else
  # Evict the launchd KeepAlive job. bootout on an absent job is not an
  # error worth stopping for — the job may already be gone, which is the
  # state we want.
  if command -v launchctl >/dev/null 2>&1; then
    launchctl bootout "gui/$(id -u)/com.marcelocantos.jevonsd" 2>/dev/null || true
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
  supervisorctl restart jevonsd 2>/dev/null || supervisorctl start jevonsd
fi

supervisorctl status jevonsd jevons-vanilla || true
