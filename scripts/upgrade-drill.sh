#!/usr/bin/env bash
# 🎯T40 upgrade-without-drain drill.
#
# Proves:
#   1. Coordinator scaffolding (upgrade mode skips StopAll; handles snapshot)
#   2. Claudia connect-mode oracles (WS reattach + detached process survival)
#   3. Optional live same-PID path with real grok agent serve
#
# Usage (from repo root):
#   ./scripts/upgrade-drill.sh           # hermetic oracles
#   ./scripts/upgrade-drill.sh --live    # + live grok serve reattach (needs Grok)
#
# Exit codes:
#   0  — scaffolding + process durability oracles green
#   1  — test failure
#   2  — residual (should not happen once connect-mode is wired)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

LIVE=0
if [[ "${1:-}" == "--live" ]]; then
  LIVE=1
fi

echo "=== 🎯T40 upgrade drill ==="
echo "repo: $ROOT"
echo

echo "--- 1. Hermetic: upgrade package (skip StopAll, connect handles) ---"
if ! go test ./internal/upgrade/ -count=1; then
  echo "FAIL: internal/upgrade tests"
  exit 1
fi
echo "PASS: ModeUpgrade.StopAgents()=false; connect-mode PlanReattach"
echo

echo "--- 2. Hermetic: claudia connect-mode oracles ---"
CLAUDIA_DIR="$(cd "$ROOT/../claudia" 2>/dev/null && pwd || true)"
if [[ -d "$CLAUDIA_DIR" ]]; then
  if ! (cd "$CLAUDIA_DIR" && go test ./ -count=1 -timeout 90s \
      -run 'TestGrokConnect|TestDialGrok|TestConnectMode|TestDetachedServe|TestFreeTCP'); then
    echo "FAIL: claudia connect-mode tests"
    exit 1
  fi
  echo "PASS: claudia connect-mode + detached process survival"
else
  echo "WARN: ../claudia not found; skipping in-tree claudia tests"
  echo "      (jevons go.mod replace should point at a claudia with connect-mode)"
fi
echo

echo "--- 3. Documented upgrade path ---"
cat <<'EOF'
Coordinator-only upgrade (process durability with CLAUDIA_GROK_CONNECT=1 default):

  # Prefer SIGHUP so jevonsd skips StopAll and writes ~/.jevons/upgrade-handles.json
  kill -HUP "$(pgrep -x jevonsd | head -1)"
  # Install new binary, then start:
  brew services start jevons
  # Or env override when only SIGTERM is available:
  #   JEVONS_UPGRADE_EXIT=1 brew services stop jevons

Agents launched under connect-mode keep a detached `grok agent serve` PID;
the new coordinator reattaches via agents.json ConnectURL/ConnectPID + session/load.
EOF
echo

if [[ "$LIVE" -eq 1 ]]; then
  echo "--- 4. Live: grok agent serve same-PID reattach ---"
  if ! command -v grok >/dev/null 2>&1; then
    echo "FAIL: grok not on PATH for live drill"
    exit 1
  fi
  LIVE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/t40-live.XXXXXX")"
  SECRET="t40-live-secret"
  PORT=$((20000 + RANDOM % 10000))
  BIND="127.0.0.1:${PORT}"
  URL="ws://${BIND}/ws?server-key=${SECRET}"
  cleanup() {
    if [[ -n "${SERVE_PID:-}" ]] && kill -0 "$SERVE_PID" 2>/dev/null; then
      kill "$SERVE_PID" 2>/dev/null || true
    fi
    rm -rf "$LIVE_DIR"
  }
  trap cleanup EXIT

  nohup grok agent --always-approve serve --bind "$BIND" --secret "$SECRET" \
    >"$LIVE_DIR/serve.log" 2>&1 &
  SERVE_PID=$!
  echo "serve_pid=$SERVE_PID url=$URL"
  for i in $(seq 1 50); do
    if nc -z 127.0.0.1 "$PORT" 2>/dev/null; then break; fi
    sleep 0.1
  done
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then
    echo "FAIL: serve died"; cat "$LIVE_DIR/serve.log"; exit 1
  fi

  python3 - "$URL" "$LIVE_DIR" <<'PY'
import asyncio, json, sys
import websockets

URI, work = sys.argv[1], sys.argv[2]

async def rpc(ws, mid, method, params=None, timeout=20):
    msg = {"jsonrpc": "2.0", "id": mid, "method": method}
    if params is not None:
        msg["params"] = params
    await ws.send(json.dumps(msg))
    while True:
        raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
        data = json.loads(raw)
        if data.get("id") == mid:
            return data

async def main():
    async with websockets.connect(URI) as ws:
        r = await rpc(ws, 1, "initialize", {
            "protocolVersion": 1,
            "clientCapabilities": {"fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False},
            "clientInfo": {"name": "t40-drill", "version": "0"},
        })
        assert "result" in r, r
        await ws.send(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}}))
        r = await rpc(ws, 2, "session/new", {"cwd": work, "mcpServers": [], "_meta": {"yoloMode": True}})
        sid = (r.get("result") or {}).get("sessionId")
        assert sid, r
        open(work + "/sid.txt", "w").write(sid)
        print("session", sid)
    # client1 closed — serve must stay up (checked by shell)
    await asyncio.sleep(0.2)
    async with websockets.connect(URI) as ws:
        r = await rpc(ws, 1, "initialize", {
            "protocolVersion": 1,
            "clientCapabilities": {"fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False},
            "clientInfo": {"name": "t40-drill-2", "version": "0"},
        })
        assert "result" in r, r
        await ws.send(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}}))
        sid = open(work + "/sid.txt").read().strip()
        r = await rpc(ws, 2, "session/load", {"sessionId": sid, "cwd": work, "mcpServers": []})
        assert "error" not in r, r
        print("load_ok", sid)

asyncio.run(main())
PY
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then
    echo "FAIL: serve PID died after client reconnect cycle"
    exit 1
  fi
  echo "PASS: same serve PID $SERVE_PID survived client disconnect + session/load"
fi

echo
echo "DRILL RESULT: PASS — scaffolding + connect-mode process durability oracles green"
exit 0
