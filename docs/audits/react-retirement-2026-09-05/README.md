# React runtime retirement record

This records T540.2/T557.1 runtime retirement, not full fidelity completion.
The reference UI is frozen at `8dd6e1694bbfb9ca1ac335f2c2d6ca939ce30fab`;
the reviewed audit/map is committed at `bfd890f706780352e5dd0da8b6c67f320d222641`.
The standing-suite inventory uses `bc303b0dc044695e0757864022bbe6197f9b7f9f`,
the subsequent target-definition commit before implementation.

## Retained obligations

`legacy-suites.json` names every former Makefile `test-web`/`test-ui` suite,
its frozen Git blob, test labels, assertion locations and explicit open
T540.3 obligation. **All assertions in each whole source blob remain open**,
including those not captured by the navigation index. The standing accounting
check derives the suite list from the frozen Makefile, verifies source blobs,
and rejects omissions. It does not count these suites as passing React tests.
Later coverage or supersession decisions must cite exact successor assertions
or reviewed applicability evidence. The main-derived shared-sidebar carveout
continues to govern; the old sidebar's divergence is not an acceptance target.

`make test-web` now runs the React unit net. `make test-ui` runs built React
main/sidebar send/reload checks and real query deep-link selection, with mock
transport clearly labelled. The existing broader React family checks retain
their explicit skips. Their gaps remain open; this milestone does not turn
skips into product evidence.

## Runtime mechanisms removed and replacements

| Removed mechanism | Preserved guarantee / intentional retirement |
|---|---|
| `web/` HTML/JS/assets and embedded vanilla package | React owns its SVG asset and has no imports of the old modules. Frozen Git source preserves behavior obligations. |
| `internal/server/devserver.go` and its index tests | Embedded React serves root/assets with cache headers; `noCache` moved unchanged into the React server. |
| `index_guard.go` | Bundle registration rejects missing JS/CSS/images and invalid local paths; HTML comments do not invent loads. |
| `tree_settle.go` and its tests | No product route reads a mutable checkout. Exact archive-content, stale-bundle rejection and unrelated-directory serving tests replace settling a moving directory. |
| `vanilla_proxy.go` and its test | Optional comparison document/proxy removed; the canonical mux keeps API/MCP/WS namespaces authoritative. No fleet or state implementation was in that proxy. |
| Production old-pixel-fixture branches | Tests feed the normal APIs/components. The removed fixture tests checked special offsets rather than owner behavior. |
| Journey Vite fallback | Invalid packaged root fails closed. J30 must use the daemon's own embedded application. |
| Standing vanilla supervisor template and startup flags | Only the React daemon remains a supported product runtime. Installed-service retirement and development activation require separate observed evidence. |

An independent read-only review traced the deletion set and found no
orchestration, persistence, authentication or provider implementation within
it. `internal/treeguard`, `commitscope`, `buildsnap`, configuration watching
and backend conversation APIs are retained. The audit frontend root moved
from `web/scripts` to `ui/src` so automated audits still inspect the product.

## Evidence gathered during implementation

- Canonical type-checked build and React tests passed on clean `96884465`
  (`5ca52654`); deliberate invalid TypeScript was rejected (`0c6c9393`).
- New asset guards passed (`a3b2e059`), including comment and traversal cases.
- Replacement standing unit/browser net passed (`c7cabe1f`): 319 unit tests
  passed, 49 remained skipped; both shared-widget browser interactions passed.
- A copied daemon binary served the actual React composer from an unrelated
  isolated working directory, received a fresh terminal reply from effective
  provider Grok, and preserved the conversation after reload. J30 plus J5
  passed (`66803357`). The first run correctly remained RED (`75152d4c`)
  because J5 was inspecting obsolete JSONL; J5 now reads SQLite without
  creating or migrating evidence, with negative controls for absent, empty
  and unrelated-agent stores.

These packaging/browser runs measured the implementation working tree, not a
clean final revision. Clean final gates, local integration, installed-service
retirement and observed development activation remain required before T540.2
can be achieved. No remote publication is part of this milestone.

The clean real-provider run passed on `0e949424` (`7a444a23`). Its full clean
Go run (`a90bcb58`) caught three fleet-brief formatting failures caused by
new bare target IDs; these were corrected without weakening the tests.
The follow-up also removes stale runtime guidance and makes the activation
script use the existing supervisord owner's SIGHUP path instead of racing
its replacement with a second daemon. Targeted brief and supervisor ownership
negative controls passed (`1641c916`). Final clean verification is still
required for the resulting revision.
