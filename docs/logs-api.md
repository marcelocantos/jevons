# Product log API

The development daemon serves the append-only product journal through read-only HTTP. These routes are intended for operators and Sentinel. They read the same journal as `jevons_logs_tail`.

`GET /api/logs` returns newest-first events. It defaults to 100 `source=server` events so browser history hydration cannot consume the operator's window. Add `source=browser` for browser telemetry or `source=all` for both. Optional exact filters are `component`, `decision`, `source`, and `level`; `q` matches message text without case; `since` and `until` are inclusive RFC3339 timestamps. `limit` is 1–2000. Invalid or repeated parameters return HTTP 400.

The response keeps `events`, `count`, and `path`, and adds `head_cursor`, `next_cursor`, and `scanned_bytes`. Pass `next_cursor` as `before` to request older events. A cursor is a byte offset into the append-only journal, so new appends do not reorder or duplicate an older page. `next_cursor: null` means the beginning was reached. Each page scans at most 4 MiB; a selective filter can return fewer than `limit` rows while still providing another cursor. Continue paging until the cursor is null. A non-null cursor does not assert that an older match exists.

`GET /api/logs/stream` is a Server-Sent Events stream for newly appended matching rows. It accepts the same filters and an optional `after` byte cursor. Without `after` or `Last-Event-ID`, it starts at the current journal end. Each `log` event has JSON in `data` and the offset after its row in `id`. Reconnect with that `id` in `Last-Event-ID` or `after`; the query parameter takes precedence. An invalid or stale cursor returns HTTP 400. The stream scans at most 2 MiB and emits at most 100 matching events per poll; it has no unbounded in-memory queue. A slow client can be disconnected and resume from its last event ID.

For a snapshot followed by new events, read a page and start the stream at its `head_cursor`. The page and stream together cover the snapshot boundary without repeatedly scanning the whole journal. A stream is a diagnostic feed, not a retention guarantee: if the journal is replaced or truncated, re-read a page for a fresh cursor.
