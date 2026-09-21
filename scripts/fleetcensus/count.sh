#!/bin/bash
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
#
# 🎯T766.1: the baseline the parent's deletion criterion is judged against.
#
# 🎯T766 is not achieved by working code. It is achieved when the count of
# mechanisms goes down — because the failure mode being fixed is the habit of
# adding one. "No file has ever shed a target reference, and no control loop
# has ever been deleted" was true of this repo on 2026-09-21, and a reconciler
# landed beside the existing twenty loops would make that worse rather than
# better.
#
# So the criterion has to be countable by a machine, not argued in a review.
# Run this, compare against docs/fleet-census.md, and the direction is a fact.
#
# Counts are deliberately crude and stable rather than clever: a grep whose
# meaning drifts is worse than no ratchet at all. Each one is an upper bound
# on a real thing, and what matters is the trend, not the absolute.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 1

prod() { git ls-files 'internal/*.go' 'cmd/*.go' | grep -v '_test\.go$'; }

# Standing control loops: a ticker or a repeating timer in production code.
loops=$(prod | xargs grep -l 'time\.NewTicker\|time\.Tick(' 2>/dev/null | wc -l | tr -d ' ')

# Seat-state derivations: production functions that decide what a seat is
# doing. Named rather than pattern-matched, because the whole point is that
# they do not share a vocabulary — a pattern would miss exactly the ones that
# matter.
derivations=0
for sym in ClassifyPhaseFile ClassifyAgentSessionPhase ClassifyAgentPhase \
           statusFromEntries seatIsBornStuck SweepDeadAgents \
           'Tracker) Sources' 'panecensus' 'fleetintent.Snapshot' \
           'PromptInFlight()' 'proc.Alive()'; do
  if prod | xargs grep -l -- "$sym" 2>/dev/null | head -1 | grep -q .; then
    derivations=$((derivations + 1))
  fi
done

# Files named after a single target: a fix that could not be expressed inside
# the thing it fixed.
named=$(git ls-files 'internal/*.go' 'cmd/*.go' | grep -v '_test\.go$' |
  grep -cE '/t[0-9]+([._][0-9]+)*_' || true)

# Substring classification: the surface every future special case attaches to.
contains=$(prod | xargs grep -o 'strings\.\(Contains\|HasPrefix\|HasSuffix\|EqualFold\)(' 2>/dev/null | wc -l | tr -d ' ')

# Target references in production code: the project's own record of accretion.
refs=$(prod | xargs grep -o '🎯T[0-9][0-9.]*' 2>/dev/null | sed 's/.*🎯//' | sort -u | wc -l | tr -d ' ')

printf '%-28s %s\n' \
  "standing_loops"        "$loops" \
  "seat_state_derivations" "$derivations" \
  "target_named_files"    "$named" \
  "substring_classifiers" "$contains" \
  "distinct_target_refs"  "$refs"
