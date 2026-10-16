// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { displayRows, isUserFrame, type DisplayRow } from './display';

/**
 * User-turn cap for the React inspect pane (🎯T609).
 * Named constant — not a magic number. Matches server historyReplayTurns.
 */
export const INSPECT_HISTORY_TURNS = 30;

/**
 * Keep the last `userCap` user-turn frames and everything after that cut.
 * A cap ≤ 0 is a no-op (the whole tape), so a removed bound cannot hide
 * behind a silent zero.
 */
export function tailInspectFrames(frames: unknown[], userCap = INSPECT_HISTORY_TURNS): unknown[] {
  if (userCap <= 0 || frames.length === 0) return frames;
  let users = 0;
  let start = 0;
  for (let i = frames.length - 1; i >= 0; i--) {
    if (!isUserFrame(frames[i])) continue;
    users += 1;
    if (users >= userCap) {
      start = i;
      break;
    }
  }
  return frames.slice(start);
}

/** Compact inspect fold: bounded window, inspect-origin paint. */
export function inspectDisplayRows(frames: unknown[]): DisplayRow[] {
  return displayRows(tailInspectFrames(frames), { inspect: true });
}
