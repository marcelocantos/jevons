Checkpoint 3 — code landed in the working tree, builds green, not yet committed or tested.

**Done this turn:**
- New `internal/seatactivity` — `Locate` (provider map: Claude `SessionExists`/`SessionJSONLPath`, Grok `discovery.GrokUpdatesLookup`, Cursor unobservable) and `Lookup` (one `os.Stat`, no decode), plus `DefaultRoots`. Unknown on unresolved path or stat failure; future mtime clamps to age 0 rather than going negative.
- `internal/mcpserver/transcript_existence.go` now delegates its provider map to `seatactivity.Locate`; `DefaultSessionRoots` delegates to `seatactivity.DefaultRoots`. One seam, not two.
- `agentInfo` gained `transcript_activity`, `transcript_last_move`, `transcript_age_seconds` (`*float64`, no `omitempty`, so unknown serializes `null` — never 0), `transcript_activity_reason`; `decorateSeatActivity` + `SetTranscriptRoots` on the server; handler calls it.
- `go build ./internal/server/ ./internal/mcpserver/ ./internal/seatactivity/` green.

**Next turn:** wire `srv.SetTranscriptRoots(sessionRoots)` at `cmd/jevonsd/main.go:298`, write `internal/seatactivity/activity_test.go` and the handler test (10s/45min fixtures, plus the mutation checks: drop-the-field and substitute-`now` must go RED), run `bin/gate -- go test ./internal/seatactivity/ ./internal/server/ ./internal/mcpserver/ -count=1`, commit only my paths, restart the development daemon and observe `/api/agents`.

Other workers have uncommitted hunks in `internal/mcpserver/{mcpserver,send_backlog,held_reaped_route}.go` and `bullseye.yaml` — excluded from my commit per 🎯T377.
