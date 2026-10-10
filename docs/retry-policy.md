# Provider turn retry policy

Jevons does not set a shared default budget for provider turn-step retries.
The selected CLI or provider/harness owns that loop; its version and settings
determine the budget. A default of one across Claude, Codex, and Grok is not
established by Jevons configuration or the Claudia v0.52.0 dependency.
An unreported budget is unknown, rather than assumed to be one.

When the transport reports exhaustion (including `retry_exhausted`,
`retries exceeded`, or `max retries exceeded`), `internal/agenterr` classifies
the outcome as `retry_exhausted` before examining the underlying timeout or
backend failure. Owner copy retains the reported detail: for example,
`max retries exceeded after 1 retry: dial tcp timeout` keeps `after 1 retry`
and states that the turn ended without finishing. This is not a successful
turn, even if earlier steps produced useful work.

Exhaustion is not eligible for Jevons' transient-backend re-pressure policy.
Resolve the underlying failure and start a new turn explicitly. Authored
assistant prose discussing exhaustion remains prose, not transport evidence.
Production consumers include the owner chat wire (`internal/server/chat_wire.go`)
and worker task errors (`internal/mcpserver/jwork.go`).

Journey exception for T862.12: the focused hermetic owner-wire fixture injects
a terminal Claudia provider-error event and checks classification, attempt
detail, terminal stop, and authored-prose isolation. This deterministically
covers error normalization without exhausting a real provider or relying on
an outage. Provider execution and activation remain unverified under the
owner hold; this fixture is not an end-to-end journey.
