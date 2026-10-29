# T1208: cockpit dependency advisory triage

Scope: the frozen 2026-10-24 npm audit of `ui/package-lock.json` at
`2e68fe98f9e7e7b68e7554e84080de52de6e997b`. Its nine package rows aggregate
30 distinct GitHub advisories; they are not nine independent exploitable
cockpit flaws. This review used the primary GitHub advisory records, public
npm package manifests/tarballs, and the production import paths below.

## Supported dependency changes

| Package | Locked before | Locked after | Reason |
|---|---|---|---|
| DOMPurify | 3.4.14 | 3.4.17 | Fix all five frozen advisories in the installed package; see the bundled-copy limitation below. |
| Seroval | 1.6.2 | 1.7.1 | Compatible with router-core's `^1.6.2`; fixed since 1.6.3. |
| source-map-js | 1.2.1 | 1.2.2 | Compatible patch for build/test consumers. |
| Undici | 8.10.0 | 8.12.3 | Compatible with jsdom's `^8.9.0`; all frozen issues fixed by 8.12.2. |
| Vite | 8.2.2 | 8.2.4 | Supported patch on the existing 8.2 release line. Raise direct minimum to `^8.2.4`. |
| Vitest and its exact-version helper packages | 4.1.11 | 4.1.12 | Supported patch; raise direct minimum to `^4.1.12`. |

No major upgrade, dependency override, audit suppression, or forced audit fix
is used. Unrelated transitive updates were excluded. A fresh `npm ci` accepts
the resulting lockfile. The post-change audit retains only the low KaTeX row
and the Mermaid row inherited from KaTeX, described below.

## Production rendering: DOMPurify and KaTeX

`ui/src/conversation/mermaidPaint.ts` imports
`mermaid/dist/mermaid.min.js?url`, loads that prebuilt browser script, calls
`initialize` with `securityLevel: 'strict'`, and renders diagram source strings
to SVG strings inserted into the transcript. Diagram source can be untrusted;
sanitization is a real product boundary.

**The shipped Mermaid 11.17.1 asset embeds DOMPurify 3.4.12.** It does not load
the lockfile's separate DOMPurify package. Inspecting the published 11.17.2
tarball found the same embedded version. Updating the lockfile therefore
does not patch the browser sanitizer, and a clean npm audit cannot prove that
it did. This change retains the existing Mermaid asset rather than claiming
the browser dependency was replaced.

The bundled code sanitizes strings for labels and sanitizes the serialized
SVG again with its own fixed configuration before returning it. The final
pass uses the default HTML parser, adds SVG attributes/tags and enables the
`foreignobject` integration point; it does not use in-place or DOM-output
modes. Its attribute hooks preserve link target/rel values, not remove
parent/sibling nodes. Diagram front matter can influence some configuration,
so absence of an option in Jevons's initialize call alone is not the evidence:
the final SVG sanitizer call and the string-only input/output path matter.

| Primary advisory | Required mechanism | Applicability to the shipped renderer |
|---|---|---|
| [GHSA-p98j-92pf-mc4p](https://github.com/advisories/GHSA-p98j-92pf-mc4p) | In-place DOM sanitization with node-removing hooks. | Neither mechanism is used; the embedded 3.4.12 also predates this advisory's affected 3.4.13–3.4.15 range. |
| [GHSA-6688-9rhm-gjv2](https://github.com/advisories/GHSA-6688-9rhm-gjv2) | In-place sanitization of an attacker-supplied rawtext DOM root, then reparsing its returned content. | The renderer supplies strings, not attacker DOM roots; final SVG output is sanitized as a string. |
| [GHSA-cg2g-xh64-v4q7](https://github.com/advisories/GHSA-cg2g-xh64-v4q7), critical | Template-safe mode plus DOM output/in-place mode, an attached shadow root, and downstream template evaluation. | There is no attacker DOM/shadow-root input or downstream template evaluator on this path. The advisory's critical rating is not evidence of a critical cockpit exploit. |
| [GHSA-mrv2-3pqr-j5jr](https://github.com/advisories/GHSA-mrv2-3pqr-j5jr) | In-place mode and hooks removing a parent or sibling during traversal. | Neither is used by the final sanitizer; existing attribute hooks do not remove those nodes. |
| [GHSA-c6gr-qvv4-5v3f](https://github.com/advisories/GHSA-c6gr-qvv4-5v3f) | XHTML parser mode and an allowlist retaining SMIL `animate`/`set` plus `attributeName`. | The final SVG sanitizer uses the HTML parser and does not opt into that allowlist. Earlier configurable label sanitation does not bypass this final pass. |
| [GHSA-238p-pmpm-9mq7](https://github.com/advisories/GHSA-238p-pmpm-9mq7), KaTeX | Preexisting prototype pollution or control of the options object's prototype; inherited trust options can then affect output. The reported XSS also requires insertion without separate sanitation. | Math labels are reachable, but no input-to-prototype-pollution path was identified, and Mermaid sanitizes the final SVG separately. This is conditional non-applicability of the named advisory, not a claim that prototype pollution or all XSS is impossible. |

KaTeX's fixed 0.18.2 is outside Mermaid 11's `^0.16.47` range; npm's proposed
Mermaid major update is outside this bounded patch. Keep both low rows visible.
Reassess if Mermaid's packaging/configuration changes, a prototype-pollution
primitive is found, or another rendering path bypasses the final sanitizer.
The embedded older DOMPurify remains a maintenance limitation even though the
specific frozen exploit preconditions are absent here.

## Other runtime and development dependencies

| Package/advisories | Actual consumer and applicability |
|---|---|
| Seroval: [GHSA-jp82-f5mq-hwhp](https://github.com/advisories/GHSA-jp82-f5mq-hwhp) | TanStack router-core depends on Seroval for SSR serialization. Jevons starts with React `createRoot` in `ui/src/main.tsx`, uses a client router in `App.tsx`, and has no application call to `fromJSON`/`fromCrossJSON` or server-rendered hydration payload. No untrusted Seroval deserialization path was identified. Patch still removes the installed vulnerable version. |
| source-map-js: [GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q) | Dev-only through PostCSS and css-tree/jsdom. The attack needs hostile indexed source-map offsets. The shipped Go daemon serves static assets, not a source-map processor; builds/tests use repository inputs. Patch protects tooling without asserting a deployed endpoint existed. |
| Undici: fourteen advisories listed below | Dev-only via jsdom; `ui/node_modules/jsdom/lib/api.js` and resource loading use it. The shipped browser uses native browser networking and the daemon uses Go, not this npm HTTP client. The tests use local fixtures, not attacker-operated upstream services. Patch removes the affected implementation from the test dependency tree. |
| Vite: six advisories listed below | Build/dev tool. `ui/vite.config.ts` binds the opt-in dev server to 127.0.0.1:5173 and uses normal SPA mode. The deployed daemon serves `ui/bundle.zip`, not Vite. The file-disclosure issues can matter to a reachable developer server; loopback is not a substitute for patching. Windows editor issues do not apply to this Mac, and the custom-middleware attack does not match the default SPA configuration. Patch all six. |
| Vitest/@vitest/mocker: [GHSA-qxg6-89mg-rqv8](https://github.com/advisories/GHSA-qxg6-89mg-rqv8), [GHSA-xp26-3454-mjgp](https://github.com/advisories/GHSA-xp26-3454-mjgp) | `vitest run` in jsdom; no browser mockerPlugin dev-server endpoint is configured. Manual mock registration requires that server endpoint; the redirect bypass additionally targets Windows paths. Neither is a shipped cockpit route. Upgrade Vitest and its exact-version helper set together. |

Undici primary advisory inventory (all share the jsdom-only consumer above):

| Advisory | Mechanism |
|---|---|
| [GHSA-3wwx-pv8p-q78v](https://github.com/advisories/GHSA-3wwx-pv8p-q78v) | WebSocket decompression error handling |
| [GHSA-pmjh-fq2x-6v4x](https://github.com/advisories/GHSA-pmjh-fq2x-6v4x) | Orphaned retry response body |
| [GHSA-r53p-7pc4-xj5r](https://github.com/advisories/GHSA-r53p-7pc4-xj5r) | Retry interceptor response splitting |
| [GHSA-rfgv-xxqx-mfg5](https://github.com/advisories/GHSA-rfgv-xxqx-mfg5) | Unrequested WebSocket subprotocol |
| [GHSA-3xpg-4rpp-hhhm](https://github.com/advisories/GHSA-3xpg-4rpp-hhhm) | Unbounded response decompression |
| [GHSA-2jfj-6hjv-fm6j](https://github.com/advisories/GHSA-2jfj-6hjv-fm6j) | Shared-cache cookie disclosure |
| [GHSA-2gqq-gqf2-x968](https://github.com/advisories/GHSA-2gqq-gqf2-x968) | Dump interceptor response truncation |
| [GHSA-w293-vg96-wgc3](https://github.com/advisories/GHSA-w293-vg96-wgc3) | BalancedPool TLS option loss |
| [GHSA-8436-99hf-9mmv](https://github.com/advisories/GHSA-8436-99hf-9mmv) | Unsafe-method response caching |
| [GHSA-vp8m-p9jh-q5pm](https://github.com/advisories/GHSA-vp8m-p9jh-q5pm) | Cross-origin cache poisoning |
| [GHSA-rx4f-c7p8-82vq](https://github.com/advisories/GHSA-rx4f-c7p8-82vq) | WebSocketStream unclean close |
| [GHSA-4qv8-2p9j-2685](https://github.com/advisories/GHSA-4qv8-2p9j-2685) | EventSource parser memory growth |
| [GHSA-f72v-6w45-q472](https://github.com/advisories/GHSA-f72v-6w45-q472) | Integrity algorithm case handling |
| [GHSA-xjj6-f33q-929c](https://github.com/advisories/GHSA-xjj6-f33q-929c) | Excessive content-encoding error handling |

Vite primary advisory inventory:

| Advisory | Mechanism |
|---|---|
| [GHSA-698v-4r3x-3vcv](https://github.com/advisories/GHSA-698v-4r3x-3vcv) | Windows editor argument escaping |
| [GHSA-r825-w8mm-5pxw](https://github.com/advisories/GHSA-r825-w8mm-5pxw) | Editor flag interpretation; Vite exposure requires the documented WSL/editor/file setup |
| [GHSA-587h-7wmj-h5pm](https://github.com/advisories/GHSA-587h-7wmj-h5pm) | Windows editor path existence check |
| [GHSA-9jrq-w75r-8gcw](https://github.com/advisories/GHSA-9jrq-w75r-8gcw) | Custom middleware retaining attacker request URL for HTML transformation |
| [GHSA-rq7h-c2jc-7f22](https://github.com/advisories/GHSA-rq7h-c2jc-7f22) | Project/system path collision bypassing dev-server filesystem allowlist |
| [GHSA-vfpm-58rq-9qcg](https://github.com/advisories/GHSA-vfpm-58rq-9qcg) | Wasm query exposes four bytes outside dev-server filesystem policy |

## Verification and limits

`scripts/react-ui-test/t1208-diagram-sanitization-test.cjs` is wired into
`make test-ui`. It serves the built React assets on a random loopback port,
mocks the normal conversation transport, blocks external requests, and checks
normal diagrams, KaTeX math labels, and unsafe HTML/URL labels through the
actual bundled Mermaid renderer. It checks visible SVG geometry, preserved
expected content, absence of script/event/JavaScript-URL attributes across the containing
wrapper, SVG root and descendants, no unexpected SVG siblings, and no
execution or rendering errors. Screenshots go to temporary storage.

This is a bounded regression net, not an exhaustive XSS proof or a live-agent
journey. **Journey exception:** the change patches dependency versions without
changing provider/daemon contracts; fixed hostile source strings and the real
browser renderer exercise the affected boundary deterministically. A live
agent would only supply those same strings. No live owner state, provider,
credentials, or production service is used. Clean React tests, bundle comparison
and isolated browser results are recorded in gate evidence for the commit;
this document does not certify a pending run as passed.

The production bundle rebuild is byte-identical: Vite/Vitest and unused
transitive patches do not alter the imported prebuilt Mermaid asset or emitted
React assets. Preserve the tracked archive unchanged and verify it with
`make ui-check-bundle`; do not introduce a synthetic archive change to satisfy
the package-input hook.
