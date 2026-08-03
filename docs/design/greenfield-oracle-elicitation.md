# Greenfield oracle elicitation (🎯T31.2)

**Status:** thin instructional + pure model slice (2026-08-03).  
**Parent:** 🎯T31 (oracle-first as system property). Sibling: 🎯T31.1 (completion enforcement).  
**Doctrine source:** oracle-first skill / `doctrine.md` §4 “Eliciting intent into the seed”.

## Problem

Porting gets an oracle for free (extract the referent). **New software** has no external reference — the reference is the owner’s intent, which is incomplete, evolving, and partly tacit. Prose intent leaks the same way prose specs do. Left to the executing agent, elicitation is skipped under the incentive to start generating (rule 9: attestation ≠ execution).

## Desired process

Co-develop **alongside design** a live **oracle-coverage map**:

| Bucket | Meaning | Production? |
|--------|---------|-------------|
| **pinned** | Executable check(s) seeded from load-bearing examples | Yes (agent work gated by the oracle) |
| **fuzzy** | Intent still open | **No** — refuse production until pinned enough to test |
| **taste** | Irreducible class-3 perceptual residue | Owner accept/reject only (single gate) |
| **spike** | Exploratory on purpose, intentionally un-oracled | Not production; proportionality |

**Unit of intent transfer:** the concrete example — *when X, expect Y*. Examples are demonstrable, seed golden/property tests, and surface disagreement prose glosses.

### Spiral (not waterfall)

```
design → thin slice → owner reacts → intent sharpens → new oracle
         ↑______________________________________________|
```

Capture each sharpened intent as an executable check as the design firms. Exploratory spikes may run un-oracled **on purpose**; production retire claims may not.

### Decidable-from-taste sort (rule 1 upstream)

Opening move of elicitation: triage so the functional majority is decidable and the irreducible class-3 residue is isolated as **one** owner accept/reject. Never mix “feel” into a decidable acceptance clause (the target inherits the p of its most-perceptual clause).

### Guards

- **Proportionality:** do not force an oracle onto a spike still discovering its shape (premature hardening depreciates).
- **Goodhart (rule 6):** elicit *load-bearing* examples, not convenient ones — pin requires ≥1 example before an oracle hint sticks.
- **Independent forcer:** overseer (who did not produce the work) holds the design gate; pure helpers assist review.

## Shipped in this slice

| Surface | Role |
|---------|------|
| `internal/mcpserver/oracle_coverage.go` | Pure `CoverageMap`, spiral phases, `ClassifyDesignClause`, `ParseLoadBearingExample` |
| Fleet standing brief / persona / agents-guide / AGENTS | Instructional process doctrine |
| Hermetic unit tests | Lock map invariants + classifiers |

## Residuals

1. **Not a hard daemon block** of code generation or bullseye achieve — instructional + pure model, same residual class as T31.1.
2. **No rich generative UI** — text/`SummaryMarkdown` path only; dogfood 🎯T29 later if the map becomes a manipulated surface.
3. **[class-3] Owner validates fidelity** of the process in real design sessions — isolated human gate; not claimed by hermetic tests.
4. **Persistence / MCP tools** to load/save maps across sessions — optional follow-up when design sessions need durable maps.

## Oracle for this design itself

- `go test ./internal/mcpserver -run 'TestCoverage|TestSpiral|TestClassifyDesign|TestParseLoadBearing|TestRegionStatus|TestNilMap'`
- Doctrine surface greps (persona, agents-guide, AGENTS, FleetStandingBrief)
