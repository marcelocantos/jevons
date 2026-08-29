Locating the T328 owner-intent resume path.Reading the loader's assistant filter and the existing tests that assert "still open despite a reply", since a plain "any reply closes" rule must not break them.Checkpoint (turn-depth ceiling). Where I am:

- Resume path found: `internal/mcpserver/open_intent.go` — `ExtractOpenOwnerIntentWithLedger` (closure rules T344/T477/T512/T528) and `loadOpenIntentDialogue` (chatlog loader, which drops assistant turns that are not evidence/answer-shaped).
- Constraint discovered: `t512_kill_resume_test.go:61` asserts a bare assistant ack ("Looking into the fleet seat now.") keeps a directive open, so "any reply closes" cannot be literal; the rule will be "a later substantive (non-ack) overseer reply closes" plus "a later reply names a TargetID that is achieved in the ledger closes".

Next step: read `bullseye_get T568` and the real overseer chatlog entry for the Cursor question, then implement both rules, add the three tapes, run `bin/gate -- make test-go`, commit `--only`, achieve, finish-report.
