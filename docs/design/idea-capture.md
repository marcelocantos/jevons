# Idea capture → bullseye/aside without evaporation (🎯T325.3)

**Status:** thin product path landed (durable intake + triage ceremony).  
**Parent map:** [life-and-work-org-map.md](life-and-work-org-map.md) §6.  
**Companions:** 🎯T93/T95 `target:` asides, 🎯T65 `capture:`/`aside:`, 🎯T130 filing reflex.

## Problem

Owner sparks die in main-chat scrollback. Capture must land in a **listable**
destination within one ceremony, then triage to file / park / hold / drop.
Full opportunity-cost optimisers and multi life-domain automation stay
**parked** (map §6–§7).

## Pipeline (binding)

```text
  spark (chat / aside / capture: / idea: / ambient)
       │
       ▼
  durable capture (aside, idea record, or bullseye draft)
       │
       ▼
  triage (root or light staff cycle)
       │
       ├── product-shaped → file bullseye (+ T193 spawn if Build)
       ├── needs-owner / design → park-for-design or design-discussion
       ├── life-domain parked (§7) → hold queue, no implementer
       └── drop / ignore (rare) — prefer park with reason
```

## Ceremonies

| Owner action | Durable destination | Listable surface |
|--------------|---------------------|------------------|
| `idea: …` | Idea ledger only (no fleet aside thrash) | `GET /api/ideas` / `jevons_idea_list` |
| `capture: …` | Fleet purpose=aside **and** idea ledger dual-write | RHS 💡 + `GET /api/ideas` |
| `aside: …` | Fleet purpose=aside (existing) | RHS 💡; optional MCP capture |
| `target: …` | Bullseye via T93/T95 filing | Ledger 🎯 + auto-close |
| Mid-chat / overseer | `jevons_idea_capture` MCP | Same ledger |

## Product surfaces

| Surface | Role |
|---------|------|
| `POST /api/ideas` | Intake (`text`, optional `source`, `domain`, `aside_id`) |
| `GET /api/ideas` | Listable oracle (`?disposition=inbox\|file\|park\|hold\|drop`) |
| `PATCH /api/ideas/{id}` | Triage (`disposition`, `note`, `domain`, `target_id`) |
| `jevons_idea_capture` / `_list` / `_triage` | Agent ceremony (same ledger under `state_dir/ideas.json`) |
| `internal/idea` | Pure model + atomic store |

### Dispositions

| Disposition | Next ceremony |
|-------------|----------------|
| `inbox` | Triage to file / park / hold / drop |
| `file` | Call `jevons_target_file` / `target:`; T193 spawn if Build-plane |
| `park` | Design / needs-owner; no unattended implementer |
| `hold` | Life-domain parked (map §7); capture ok; no implementer |
| `drop` | Rare noise; prefer park when unsure |

Parked life-domain tags (seed): `swot`, `education`, `finance`, `health`,
`leisure`, `entertainment`, `device-life-app`, `hardware`, `cat-flap`, …
— intake with these domains lands as **hold** automatically.

## Oracle

Hermetic preferred:

```bash
go test ./internal/idea/ ./internal/server/ -run Idea
go test ./internal/mcpserver/ -run Idea
node web/scripts/idea_capture_test.js
```

Acceptance shape: **captured idea appears in listable surface within the
ceremony** (POST → GET lists same id; MCP capture → list contains id).

## Residual (class-3 / parked)

- Full opportunity-cost dynamic optimiser
- Multi life-domain automation / SWOT automation / device life-app
- Rich owner UI for idea inbox (ledger API is the thin listable surface)
- Auto-file bullseye from disposition=file (still explicit `jevons_target_file`)

## Relation to T93/T95

`target:` remains the **filing** ceremony (name + acceptance → 🎯 +
`__TARGET_FILED__`). Idea capture is the **pre-file** bucket when the spark
is not yet a target assertion. Triage `file` bridges idea → bullseye.
