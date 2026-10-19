# Frozen CollectSpend baseline corpus (🎯T684)

Pinned input for `TestCollectSpendReproducesFrozenBaseline`.

The original 🎯T392.6 oracle walked the owner's live `~/.grok` tree over a
fixed window (2026-08-08 01:53Z .. 2026-08-09 11:53Z). That tree is not a
baseline: harness layout changes (`sessions/` appearing, throwaway workdirs,
rotation) silently replace the input, and the failure then reads as a
`CollectSpend` regression (`turns=2 want 1070`) rather than a missing corpus.

This directory is the durable replacement. The frames are synthetic but
carry the same frozen totals the live window once produced (1070 turns,
4848 model calls, 907.1M input, 120 cancelled), in the same Grok
`turn_completed` shape `cost.ParseLine` accepts. A harness layout change
cannot move or empty this tree without a deliberate repo edit.

Do not "fix" a red baseline by editing the expected totals to match a
broken collector — that is how the oracle loses its teeth. Fix the reader.
