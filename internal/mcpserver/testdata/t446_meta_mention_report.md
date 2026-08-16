🎯T439 landed and reported to jevons-po (queued behind its in-flight turn).

**Commit** `a0208665a315fb12faf71169bca8fa0d97255656` on local master, five paths only (`git commit --only`), reachable from HEAD.

**What I picked up and finished:** my predecessor had the classifier and span helpers written but uncommitted, with no tests, no log wiring, and no gate. I added the lifecycle-log evidence, the oracle file, and the verification.

- `AskRequest` — a report that names an input it is waiting on (brief resend, an answer, an unblock) is not a finish, whatever completion vocabulary the rest carries. Markers exclude finish-time courtesies so 🎯T165/🎯T195 bare-done reaping is untouched.
- `reapDecisionFields` — every `reap_done` record now names the classifier that fired, the phrase matched, and the span of report text around it. jv-t435's reap was reconstructible only from the report store.
- `internal/mcpserver/reap_request_test.go` over the real stored report (verbatim from `~/.jevons/agent-reports`, 1122 bytes).

**Evidence, both directions proven by mutation rather than assertion:** emptying `requestMarkers` reproduces the incident exactly (`outcome = ok, want skipped`); making everything a request fails the 🎯T395 rows and `TestT395ReapStillFiresForOrdinaryFinishes`.

- `GATE t439-mcpserver exit=0 GREEN id=c1c07bdc`
- `GATE t439-clean-checkout exit=0 GREEN id=0f1d4204` — detached worktree at a020866, so the green is independent of the shared tree's WIP
- `GATE t439-head-build exit=0 GREEN id=af143180`

Daemon rebuilt from committed HEAD and serving (`/health=200 /api/frontier=200`, pid 35057).

Two things worth flagging: `make test-go` is red in the shared clone on another worker's uncommitted `internal/fleet/fleet.go` edit (`cannot use fn … as func(name string) func()`), which breaks cmd/jevonsd, cmd/jevons-watchdog and internal/supervise — not mine, and not in my commit. And the residual is real: the request markers are a phrase list, so an ask worded outside it still reads as a finish, which is the incident failure mode rather than a safe one.