# React cockpit oracles (🎯T540.1)

`web/` is a **visual reference**, not a test target. The **census**
(`census.ts`) is the set of pre-React UI targets. Tests exist to prove
those retired contracts still hold on React — and to **fail** where the
migration is incomplete or wrong.

## Referent

The referent is the **retired target** (name + acceptance in the ledger),
ported from vanilla `web/` chrome (`#input`, `#messages`, `.msg.user`,
`.msg-clipped`, `#frontier-table`, `#plan-ticker`, …) as the oracle spec.
**The running React app is not the referent.** Do not write a check so
that today's `ui/` goes green. Do not grep `ui/src` for an id that
happens to exist. Do not accept a renamed class, a `data-*` stand-in, a
stub control, or a missing pocket because "that is what React ships
today."

These tests are a **gap map**, not a regression net. The premise is that
the React migration is incomplete and in places incorrect. A red suite
that names the retired contract is the product. A green suite that only
describes the current tree is a false green.

## Mix

| Layer | When | Where |
|---|---|---|
| **Hermetic** | The behaviour is a function of frames, keys, geometry, or paint classes | `ui/src/oracle/families/*.test.ts` |
| **Journey** | Owner-visible product path: isolate + real agent + Chromium | `scripts/journey-suite` — J19 plus the chrome pack (send, fold-md, composer, fleet, aside, frontier, ticker) |

A journey **must interact with an agent** (🎯T107). Seeding a journal and
hard-loading the isolate cockpit counts. Grepping `ui/src` does not.

After T540.2, isolate `GET /` is React when `ui/dist` exists; otherwise
the shared Vite-proxy helper loads React (never `:13705`). Dual-path
residual named.

Journey-or-hermetic: a named hermetic is enough when the vertex is a
pure function of frames/keys. Journeys are the arbiter for connect,
send, fleet, aside, frontier, ticker-in-the-page, and anything jsdom
cannot see. `itOracle.skip` is only for "journey is the arbiter" or a
true exception — never for "React does not do this yet."

## One family, one file

`census.ts` is the set. `catalog.ts` is the runner map. Each row is one
file under `families/`.

```ts
describeOracle(family('fold'), () => {
  itOracle('T504', 'user bubble is a stream barrier', () => { /* predicate from T504 */ });
});
```

- `itOracle` first argument is a **retired id** from `covers`.
- Assert the acceptance text, using vanilla constants (14rem clip,
  `.msg-clipped`, `#input`, stream barrier). If React disagrees, fail.
- Do **not** screenshot-diff. Visual feel is a T493.1 prose look on
  paint journeys.

## Isolation

- Do not edit `catalog.ts` / `census.ts` unless you are the harness owner.
- Do not edit `Makefile`, `bullseye.yaml`, or `web/`.
- Commit with `git commit --only` (🎯T377).
