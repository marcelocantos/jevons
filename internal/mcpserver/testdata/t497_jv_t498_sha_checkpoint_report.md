Checkpoint — commit is landed, only the gate run remains.

**State:** Fix applied and committed on local master as `8c22aef7d1db1669e48e2629bc312ca8ff1f1a2a` — replaced the last `typeof ConversationWidget !== 'undefined' &&` ternary in `web/index.html`'s `attachTranscriptRow` turn-slot branch with a direct `ConversationWidget.createTurnMarkerEl(...)` call (1 insertion, 5 deletions; `git show --stat HEAD` confirms only `web/index.html`). The `virtual_list_test.js` 700-char window invariant was verified to still hold (`createTurnMarkerEl` present, `buildMsg(row.role` absent).

**Next step (single command, next turn):** in the detached worktree at `scratchpad/t498-wt`, `git checkout --detach 8c22aef`, confirm zero local edits, then `bin/gate -- make test-web` and cite the GATE GREEN line in the finish report.

Still in progress — acceptance gate not yet run.