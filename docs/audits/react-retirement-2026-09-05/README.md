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

Those initial packaging/browser runs measured the implementation working tree,
not a clean final revision. The final evidence below supersedes their pending
verification status. No remote publication is part of this milestone.

The clean real-provider run passed on `0e949424` (`7a444a23`). Its full clean
Go run (`a90bcb58`) caught three fleet-brief formatting failures caused by
new bare target IDs; these were corrected without weakening the tests.
The follow-up also removes stale runtime guidance and makes the activation
script use the existing supervisord owner's SIGHUP path instead of racing
its replacement with a second daemon. Targeted brief and supervisor ownership
negative controls passed (`1641c916`).

## Final desktop verification and local activation

The following gate records were read back from the gate store, rather than
inferred from a command runner's status:

| Committed revision | Gate | Scope and result |
|---|---|---|
| `68dc7dccc6cb` | `bc34b904`, GREEN | `make ui-check-bundle test-web test-ui test-go`: canonical TypeScript build and exact bundle match; 319 React tests passed, 49 skipped; 87 frozen suites accounted for as open obligations; built React browser checks passed; 3,117 Go tests passed, 4 skipped across 88 packages. |
| `68dc7dccc6cb` | `52fe6ce9`, GREEN | Packaged J30 and J5, effective provider Grok. The actual composer elicited `react-9ed2b8b1-d10e-4bd2-bb1e-25ac11775b5d` in a `transcript:jevons` assistant frame with `stop_reason=end_turn`; reload retained it. The independent SQLite isolation check passed. |
| `c8eefd3ec7f7` | `859cb5cf`, GREEN | Supervisor installation and upgrade tests after the final installer fix: apply the rendered group with `update jevonsd` before restart, including fresh and changed definitions. |

The normal and negative type-check evidence supports T624. The standing React
net and source-derived frozen-suite accounting support T557.1. None of the 49
skipped React tests or 87 retained legacy obligations are claimed as product
coverage. These runs are not a claim that the entire journey suite passed.

Local `master` was fast-forwarded to `c8eefd3ec7f7`. Other workers' changes
were preserved: 101 unrelated files retained their content hashes, and 18
overlapping surviving edits were restored. Nine edited legacy files that the
migration deletes remain recoverable in preservation stash
`cae853507547f0d98903c3e342b724d6b96d9f86`; they were not restored as a runnable
second UI. No push, pull request or release was performed.

The installed vanilla supervisor group was stopped and removed. Its installed
configuration and old launchd comparison plist are absent, and port 13706 has
no listener. During this scoped retirement, the development daemon's PID,
configuration hash and two fleet seats were unchanged.

The subsequent development activation used the actual supervisord owner and
compiled committed Jevons `c8eefd3ec7f7` through buildsnap. It took about four
minutes, so this is not an uninterrupted-service claim. Operational gate
`0fe88bd0` was GREEN; HTTP health and frontier responses were observed afterward.
The existing sibling-Claudia workspace injection remained in use; this is not
evidence of a build against only the published dependency pin.

A browser observation of the real development surface loaded and reloaded
the main transcript and the `?agent=jevons-po&tab=transcript` deep link without
mock transport or sending messages. It observed 86 main rows and 35 PO rows,
with no uncaught browser errors. Root and all six embedded archive entries
were byte-identical to `ui/bundle.zip`. The transcript screenshots showed
populated chat panes, no Latest button, and little empty canvas. They also
showed a degraded-overseer banner during recovery: these captures establish
history/rendering, not a healthy final fleet. Jevons subsequently resumed its
existing Cursor conversation; the PO recovery gap below remains.

## Outstanding completion requirements

**T540.2 remains open.** The final desktop checks exposed two concrete gaps:

- **T628, iOS canonical transport:** the wrapper still loaded a `web/` bundle,
  and its paired bridge spoke the old chat protocol rather than serving React
  assets, HTTP APIs and `/ws/mux`. `make ios` only generates the Xcode project;
  it passed despite the missing resource. The actual simulator build failed
  (`9b3068a1`, exit 65) after the loose Pigeon requirement resolved 0.32.0 and
  required unavailable `sodium.h` and `ngtcp2/ngtcp2.h`. The direct-loading slice
  below addresses part of this gap; paired transport and observed mobile
  journeys are still required. Removing a desktop runtime does not retire
  mobile support.
- **T629, Cursor PO recovery:** Cursor rejected saved PO conversation
  `b54f134f-f7ef-4780-a077-37132cd64d14` with `session/load: Invalid params`.
  Jevons correctly refused to silently mint a replacement. The named transcript
  remains available, but the PO is stopped. Missing provider files and
  `materialized=false` cannot prove a persisted conversation was unused.
  Automatic recovery requires positive unused-mint evidence; recovery of this
  legacy row requires an explicit cold-recovery decision preserving its old ID,
  identity, lineage and durable transcript.

The PO problem is a separately tracked backend repair, not an added broad
parity prerequisite. It is recorded here because activation exposed it and
the observed fleet state must not be reported as fully restored.

## iOS direct-path progress

The iOS working slice pins Pigeon exactly to 0.19.0, matching the daemon's
dependency; SwiftPM resolved revision `475b9970a9f9ceb80990d1cefa0c04f873f8cb01`.
It removes the deleted `../web` resource, loads the daemon URL in WKWebView,
and gives that cockpit precedence over the old native view tree. Endpoint
changes navigate to the new URL. Failed initial navigation retries with a
capped delay; ordinary state updates do not reload an already loaded page.
Simulator launches can select an isolated endpoint with `JEVONS_SERVER_URL`.

The initial simulator build was GREEN `b85ca077`; the navigation-recovery
follow-up was GREEN `95201d45`. Both measured uncommitted iOS changes on
`c8eefd3ec7f7`, not a clean final revision. Existing Swift warnings remained.

On the reserved iPad mini simulator, the built app rendered the actual
development React surface. A second observation launched it against a
deliberately offline loopback endpoint, then enabled a byte-forwarding TCP
proxy to the untouched development daemon. The same app process recovered
from the blank initial navigation to populated React history and fleet panes
without a relaunch. No messages were sent and no API replies were fabricated.
The resulting viewport contains a long main transcript and the fleet/frontier
pane, with no Latest button or degraded-overseer banner. It demonstrates
canonical loading and recovery; the narrow main pane is not a claim of final
mobile layout acceptance.

Visual verdict for the mobile viewport: **no**, this is not yet evidence of
a normally usable mobile chat transcript. Although history is populated and
the degraded banner is absent, the composer is partly below the captured
bottom. Keyboard, sending and responsive layout remain unverified.

This is manual render/recovery evidence, not a mobile agent journey. The
paired branch remains incomplete and these iOS edits are not a releasable
mobile migration. T628 and T540.2 remain open until both supported transports
carry the canonical UI and satisfy the real-agent/reload criteria.
