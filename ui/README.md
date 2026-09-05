# React cockpit (🎯T540)

React is the only product cockpit. The daemon embeds the tracked
`ui/bundle.zip`; development and released binaries serve the same application
without reading the checkout. `ui/dist` is an ignored build intermediate.

```bash
make ui-build            # TypeScript + Vite + deterministic embedded bundle
make ui-check-bundle     # non-mutating check that the tracked bundle is current
make jevonsd             # canonical daemon build, including React
make ui-dev              # optional Vite HMR while editing
make test-web            # React unit tests (historical command name)
make test-ui             # built React main/sidebar browser checks, mocked wire
make test-journey        # real agents; J30 sends through the packaged composer
```

There is no standing comparison service on :13706. The historical reference
is Git commit `8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab`; the reviewed audit is
`docs/audits/react-fidelity-2026-09-05.md`. Retiring vanilla does not establish
full fidelity. Former standing assertions remain explicitly open in
`docs/audits/react-retirement-2026-09-05/legacy-suites.json` under T540.3.
The old sidebar's divergent semantics are not the reference: main and sidebar
share the React components and main-derived interaction model.

Parity oracles: `src/oracle/methodology.md` and `src/oracle/families/`.

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
