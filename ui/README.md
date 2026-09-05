# React cockpit (🎯T540 / 🎯T540.1)

Vite + React 19 — **product** owner cockpit. Vanilla `web/` is deprecated
reference-only; port parity gaps here, do not patch vanilla for features.

Daily `:13705` `GET /` serves this build (`make ui-build` → `ui/dist`),
supervised by LaunchAgent `com.marcelocantos.jevons-ui`. Vanilla `web/`
is LaunchAgent `com.marcelocantos.jevons-ui-vanilla` on `:13706`.
`make ui-dev` is opt-in HMR, not a standing agent.

```bash
make ui-daemon-install   # two LaunchAgents on :13705 and :13706
make ui-dev              # opt-in Vite HMR (not launchd)
make ui-build            # writes ui/dist for daily GET /
make test-ui-react       # or: cd ui && npm test
```

Parity oracles: `src/oracle/methodology.md` — family files under
`src/oracle/families/`. Vanilla `web/` is a visual reference, not a
test target.

One conversation API: WebSocket `/ws/mux`, channel `transcript:{name}`.
Root, PO, and workers are the same `AgentInteraction`.

`make ui-build` is the canonical acceptance build: it installs locked
dependencies when needed and runs `npm run build` (TypeScript checking, then
Vite). The full product command and CI run this build. `npm test` runs Vitest;
it does not type-check. Use `make ui-dev` for fast local HMR, and the canonical
build before accepting a change. Type-only cleanup under 🎯T624 preserves
runtime behavior; its checks are the canonical build, existing React tests and
a deliberately invalid TypeScript input rejected by that same build, rather
than a new owner journey.
