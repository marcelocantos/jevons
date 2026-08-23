// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Distance from the live end. */
export function distanceFromEnd(scrollTop: number, scrollHeight: number, clientHeight: number): number {
  return Math.max(0, (Number(scrollHeight) || 0) - (Number(scrollTop) || 0) - (Number(clientHeight) || 0));
}

/**
 * 🎯T351: assign this as scrollTop when pinning. Over-assign the full
 * scrollHeight — the browser clamps to the fractional max. Integer
 * sh − ch leaves a residual that grows when late measures add height.
 */
export function pinWriteScrollTop(scrollHeight: number): number {
  return Math.max(0, Number(scrollHeight) || 0);
}

export const FOLLOW_END_PX = 80;

/**
 * Whether a scroll event should rewrite follow. Programmatic pin writes
 * must not drop track: the first pin uses estimated row heights, then
 * the last bubbles measure taller and fromBottom jumps (often ~½–⅔ of
 * the pane) before the next pin can run.
 */
export function shouldHoldFollow(opts: {
  fromBottom: number;
  pinning: boolean;
  threshold?: number;
  /** Prefix grew (hydrate remat / live append). Not a user leave. */
  heightGrew?: boolean;
  wasFollowing?: boolean;
}): boolean {
  if (opts.pinning) return true;
  // Measure growth moves the live end away from the current scrollTop.
  // That looks like a leave if we only read fromBottom (often ⅓–⅔ pane).
  if (opts.wasFollowing && opts.heightGrew) return true;
  const t = opts.threshold == null ? FOLLOW_END_PX : opts.threshold;
  return opts.fromBottom < t;
}
