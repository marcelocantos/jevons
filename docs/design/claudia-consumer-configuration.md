# Jevons configuration for a separate Claudia broker

Claudia's broker is a separate shared process. Setting an environment variable
inside `jevonsd` does not reconfigure its sidecar. The neutral Claudia defaults
use `$XDG_STATE_HOME/claudia/spool` (or `~/.local/state/claudia/spool`), seed no
MCP credentials, and reserve no MCP server name prefixes for consumers.

The existing Jevons deployment must explicitly retain its current spool path
when this dependency change is activated. Its transcript, activity and legacy
cost readers continue using `~/.jevons/spool`; there is no file migration.

## Broker launch configuration to apply at activation

This is the reviewed configuration artifact, not an installed service change.
Use the selected exact Claudia executable and retain that deployment's existing
socket, state and source settings. Add these settings to the **broker** launcher:

```sh
CLAUDIA_OMP_SPOOL_DIR="$HOME/.jevons/spool" \
  /absolute/path/to/claudia broker serve \
  -mcp-seed-dir "$HOME/.jevons" \
  -mcp-consumer-prefix jevonsmcp
```

The seed directory is consulted only when the broker's corresponding MCP
store is empty. Existing broker tokens and remembered upstreams win. The
prefix keeps Jevons's per-consumer MCP URL direct instead of registering it
under the shared broker's name-keyed proxy.

For supervisord, the equivalent environment entry is
`CLAUDIA_OMP_SPOOL_DIR="%(ENV_HOME)s/.jevons/spool"`; the two MCP flags must
reach the final `broker serve` command. The current Claudia wrapper does not
forward arbitrary arguments, so appending flags to that wrapper is insufficient.
For a launchd service, use the absolute expanded spool path in
`EnvironmentVariables` and add the two flag/value pairs to `ProgramArguments`.
Configure the one selected service; this document does not activate another
broker or modify either installed service definition.

Keep Jevons's own `JEVONS_SPOOL_DIR` reader override when one is configured, and
use that exact path for the broker's neutral override. No Claudia component
reads `JEVONS_SPOOL_DIR` after this change.

The journey suite passes both variables explicitly to the same disposable
spool, seeds only its private state directory, and supplies the consumer prefix
on the actual isolated broker command. It does not seed from the owner's state.

## Verification boundary

Hermetic checks cover Go/Bun path agreement, actual spool write/read, neutral
versus explicitly seeded MCP hosts, prefix routing and the isolated broker's
constructed invocation. The production service has not been restarted and the
live fleet journey remains outstanding. Claudia's implicit tool fallback and
ambient Bullseye sandbox root remain separate cleanup work; this prerequisite
does not establish full consumer neutrality or complete 🎯T160.
