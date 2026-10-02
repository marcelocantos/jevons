# T996 selected-seat Frontier

The cockpit resolves the selected Agents-tree seat's workdir and requests
`/api/frontier?cwd=…`. No selection, an unknown seat, or a seat without a
workdir produces an empty table and no default-ledger request. Clicking the
selected seat clears the selection; the empty selection survives reload.

The query cache is scoped by workdir, consumes the abort signal, and does not
retain another repo's rows during loading or errors. Table-local kickoff state
resets on workdir changes. Engagement and stop controls use the returned ledger
key, keeping identical target IDs in different repos separate.

## Verification

- `make test-web-clean`: includes `useSeatFrontier.test.tsx` for empty selection,
  repo switching, late responses, HTTP failure, and query invalidation.
- `bin/gate -clean -- make ui-check-bundle`: checks committed source and bundle.
- Explicit development probe (real APIs and DOM, no mocks):
  `UI_HOST=http://127.0.0.1:13705 bin/gate -- node scripts/chat-ui-test/t996-selected-seat-frontier-test.js`.
  Defaults to claudia-po then bullseye-po; `SEATS` accepts two other seat names.
  It compares every target ID and displayed name with the selected repo's API
  response, clears selection, reloads, and saves screenshots in
  `/tmp/t996-frontier` (`ARTIFACT_DIR` overrides).

Journey exception: this change affects selection and data fetching, not agent
execution. The hook regression and real development selection/DOM probe cover
that boundary without submitting an unrelated agent turn. The development
probe remains required for acceptance; hermetics alone do not close T996.

Decision: clicking a selected seat deselects it because the existing tree had
no clear-selection action. An initial URL without `agent` now selects nothing.
