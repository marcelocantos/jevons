CHECKPOINT — ending this turn at the depth ceiling (🎯T392.4). This is not a finish; work on 🎯T445 is in progress.

Where I am:
- Read 🎯T445 acceptance: the classifier must fail safe — a worker turn matching neither request markers nor an explicit finish shape is NOT a finish; hermetic tests need ≥6 ask-shaped turns worded outside the current phrase lists (e.g. "one thing I need from you", bare question-mark ending, ask embedded mid-report), red against the pre-fix tree.
- Read the full classifier chain: `internal/mcpserver/reap_done.go`, `report_ask.go`, `oracle_evidence.go`, plus `~/.claude/go.md` rails.
- Captured real unrecognised-turn samples from jv-t497-checkpoint-reap: four 22-byte "No response requested." turns and a 148-byte mid-action turn ("The probe returned 200, but it may have hit the old daemon… Checking the restart log to confirm…").

Next steps for my successor turn:
1. `grep -rn "finished_work"` in non-test Go under internal/ and cmd/ (the call the ceiling interrupted) and `git show --stat 3fec8c5c` to see how 🎯T497 shaped the chain.
2. Read `internal/mcpserver/reap_done_test.go` and `report_ask_test.go` to match test idiom.
3. Write the hermetic oracle first (new failing tests from the real shapes + the six acceptance phrasings), then fix — likely: require a positive finish shape rather than "completion word anywhere + no recognized ask", while preserving 🎯T195 bare-done reaping.
4. Gate via `bin/gate -- go test ./internal/mcpserver/`, commit `--only` my paths, detached daemon restart, finish report with GATE GREEN + SHA.

No files modified yet; nothing committed.
