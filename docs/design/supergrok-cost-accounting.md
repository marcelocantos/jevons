# SuperGrok / subscription cost accounting (🎯T137)

Status: shipped (design + code). Supersedes the temporary full disable
of the cost subsystem when SuperGrok's API-equivalent burn paused the
fleet (2026-08-03).

## Problem

Grok / SuperGrok plans often have **no marginal per-token dollars**.
Jevons still sees provider `costUsdTicks` and table estimates that look
like paid-API list prices. Treating those as real $ caused:

- fleet pause/kill on "fake" `$/hr`
- projected daily spend over a `$500` budget while true marginal $ ≈ 0
- (before 🎯T139) overseer stop → grey cockpit, undeliverable chat

Temporary mitigation: `budget.json` `"disabled": true` — no collector,
no enforcer, no dollar UI. That is safe but leaves no re-enable path
that is honest for subscription economics.

## Model

`budget.json` fields (owner-editable, no code change):

| Field | Meaning |
|---|---|
| `disabled` | `true` → no collector/enforcer; `/api/cost` → `{disabled:true}`; UI hides $ |
| `accounting` | `list_price` (default) or `subscription` |

### `list_price` (paid API)

- USD figures are billable estimates (provider costUSD / table).
- Full warn → throttle → pause → kill + hard ceiling.
- UI shows `$N/hr` as today.

### `subscription` (SuperGrok / flat plan)

- Same collector + rates for **visibility**.
- Snapshot carries `billable: false` and
  `currency_note: "API-equivalent USD estimate — not billed…"`.
- USD rate / hard-ceiling signals **cap at warn** (informational).
- Enforcer **never** pause/kill/spawn-halt on USD ladders (defense in
  depth even if a kill-level alert is injected).
- Non-dollar hygiene still available: session-count, orphan sessions,
  collector-stale, dead-man (unattended burn), overseer protection (T139).
- UI ticker: `est $N/hr … (not billed)`.

### Recommended SuperGrok config

```json
{
  "disabled": false,
  "accounting": "subscription",
  "max_sessions": 20,
  "protected_workers": ["jevons"]
}
```

To fully silence monitoring again: `"disabled": true` (existing flag).

## Non-goals (residual)

- Token-rate / session-rate clamp ladders as a full substitute for USD
  (optional later; session-count warn already exists).
- Live SuperGrok billing API integration (no public marginal $ signal).
- Changing the owner's `~/.jevons/budget.json` automatically — re-enable
  is an explicit owner edit to `accounting=subscription`.

## Oracles

- Hermetic: `go test ./internal/cost/…` — subscription never enforces on
  USD; list_price still does; accounting fields round-trip.
- Hermetic web: `node web/scripts/cost_display_test.js` — disabled hides
  ticker; subscription labels estimates; list_price shows billable $.
- Live residual: owner flips daily `budget.json` when ready.
