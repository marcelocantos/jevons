# React cockpit oracles (🎯T540.1)

`web/` is a **visual reference**, not a test target. Product oracles live
in `ui/` and run under `make test-ui-react` (`cd ui && npm test`).

Vanilla Playwright (`make test-ui`) and `web/scripts/*_test.js` stay as
the frozen reference net. Do not add new product assertions there.

## Mix

| Layer | When | Where |
|---|---|---|
| **Hermetic** (default) | The behaviour is a function of frames, keys, geometry, or paint classes | `ui/src/oracle/families/*.test.ts` |
| **Journey** | The bug only appears after a real isolate connect / live agent | `scripts/journey-suite` — J19 is the connect-tail journey; one extra journey max unless a hermetic cannot exist |

A journey **must interact with an agent** (🎯T107). Seeding a journal and
hard-loading the isolate cockpit (J19) counts. Grepping `ui/src` does not.

Until T540.2, isolate `GET /` may still be vanilla. Journey work **retargets
J19** (and any new journey) at the React surface (`ui` build or `:5173`
proxy against the isolate), and names that residual if cutover is still
blocked. Do not write a second connect-tail journey.

## One family, one file, one worker

`catalog.ts` is the contract. Each row is one file under `families/`.
Workers do not invent runners, do not share Playwright yet, and do not
edit another family's file (🎯T376).

```ts
describeOracle(family('fold'), () => {
  itOracle('T504', 'user bubble is a stream barrier', () => { /* predicate */ });
  itOracle('T63', 'tool_use folds into steps, not a bubble', () => { /* … */ });
});
```

- `itOracle` first argument is a **retired vanilla id** from `covers`.
- One `itOracle` may list several ids only when they are the **same
  predicate** (e.g. T66+T77 same clip rule). Prefer one id per test.
- `it.todo` is allowed for a vertex you have not landed. A family is
  not done while a `covers` id has neither `itOracle` nor an explicit
  `itOracle.skip` with a one-line why (journey-or-exception).

## What to assert

Write a **decidable predicate** against React modules:

- `displayRows` / `reduceTranscript` / `classifyPace` / `modelPrefix` /
  `planComposerTabCycle` / `shouldClip` / pin geometry — prefer these.
- RTL mount (`@testing-library/react`) only when the predicate is
  classNames or focus in a real component. Use `fixtures.ts` frames.
- Do **not** screenshot-diff. Visual feel (T106 scrim, T489) is a short
  prose verdict plus a T493-class census, or `journey-or-exception`.

Vanilla `web/scripts` and `scripts/chat-ui-test` are the **oracle spec**:
read them, port the assertion, do not import them.

## Isolation

- New code goes in `ui/src/oracle/families/<id>.test.ts` and, if needed,
  a helper next to the product module (`ui/src/conversation/…`).
- Do not edit `catalog.ts` unless you are adding a family (harness
  owner). Do not edit `Makefile`, `bullseye.yaml`, or `web/`.
- Commit with `git commit --only` of your paths (🎯T377).
- Finish with a `finish-report` envelope (🎯T509) citing
  `cd ui && npm test` (or `make test-ui-react`) and the GATE line if
  you used `bin/gate`.

## Journey worker (one seat)

Owns `scripts/journey-suite` only. Retarget J19 at React; add at most
one new journey (owner send paints once + reply after the user bubble)
if hermetic fold/composer cannot see it. Portguard still refuses
`:13705`.
