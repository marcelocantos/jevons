# React cockpit (🎯T537.1)

Vite + React 19. Daily `GET /` is still `web/index.html` until an explicit
owner cutover (🎯T505).

```bash
make ui-dev    # http://localhost:5173  proxies /api and /ws to :13705
cd ui && npm test
```

One conversation API: WebSocket `/ws/mux`, channel `transcript:{name}`.
Root, PO, and workers are the same `AgentInteraction`.
