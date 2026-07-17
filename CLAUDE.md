@AGENTS.md

Claude Code adapter over the shared `AGENTS.md`. Shared rules live there;
Claude-only material lives below.

- Voice targets (anything under 🎯T21/T22/T28) are gated on the 🎯T37
  decision — don't resume voice work without it.
- The `jevons` overseer prompt is embedded in `cmd/jevonsd/main.go` until
  🎯T44 externalizes it; treat it as config-in-code when editing.
