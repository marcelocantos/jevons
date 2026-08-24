# React cockpit (🎯T540 / 🎯T540.1)

Vite + React 19 — **product** owner cockpit. Vanilla `web/` is deprecated
reference-only; port parity gaps here, do not patch vanilla for features.

Daily `GET /` may still serve `web/index.html` until cutover (🎯T505 /
🎯T540.2). Until then:

```bash
make ui-dev    # http://127.0.0.1:5173  proxies /api and /ws to :13705
make test-ui-react   # or: cd ui && npm test
```

One conversation API: WebSocket `/ws/mux`, channel `transcript:{name}`.
Root, PO, and workers are the same `AgentInteraction`.
