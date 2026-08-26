# React cockpit (🎯T540 / 🎯T540.1)

Vite + React 19 — **product** owner cockpit. Vanilla `web/` is deprecated
reference-only; port parity gaps here, do not patch vanilla for features.

Daily `:13705` `GET /` serves this build (`make ui-build` → `ui/dist`).
Vanilla `web/` remains on `:13706`. Vite HMR:

```bash
make ui-dev    # http://127.0.0.1:5173  proxies /api and /ws to :13705
make ui-build  # writes ui/dist for daily GET /
make test-ui-react   # or: cd ui && npm test
```

Parity oracles: `src/oracle/methodology.md` — family files under
`src/oracle/families/`. Vanilla `web/` is a visual reference, not a
test target.

One conversation API: WebSocket `/ws/mux`, channel `transcript:{name}`.
Root, PO, and workers are the same `AgentInteraction`.
