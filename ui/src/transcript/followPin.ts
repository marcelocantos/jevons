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

/** Scroll-handler decision: measure growth after hydrate is not a user leave. */
export function followAfterScroll(o: {
  fromBottom: number;
  pinning: boolean;
  wasFollowing: boolean;
  prevHeight: number;
  scrollHeight: number;
  clientHeight?: number;
}): { follow: boolean; height: number } {
  const sh = Number(o.scrollHeight) || 0;
  const prev = Number(o.prevHeight) || 0;
  return {
    follow: shouldHoldFollow({
      fromBottom: o.fromBottom,
      pinning: o.pinning,
      wasFollowing: o.wasFollowing,
      heightGrew: prev > 0 && sh > prev,
      clientHeight: o.clientHeight,
      prevHeight: prev,
    }),
    height: sh,
  };
}

/**
 * Re-pin abort for a mid-list viewport (🎯T556). First paint is scrollTop=0
 * on a tall canvas — the caller must not treat that as a leave (🎯T558).
 */
export function shouldAbortPinForMidList(opts: {
  fromBottom: number;
  clientHeight: number;
  scrollTop?: number;
}): boolean {
  if ((Number(opts.scrollTop) || 0) <= 0) return false;
  const ch = Number(opts.clientHeight) || 0;
  return ch > 0 && (Number(opts.fromBottom) || 0) > ch;
}

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
  clientHeight?: number;
  prevHeight?: number;
}): boolean {
  const from = Number(opts.fromBottom) || 0;
  const ch = Number(opts.clientHeight) || 0;
  const midList = ch > 0 && from > ch;
  const established = (Number(opts.prevHeight) || 0) > 0;
  // User moved more than one viewport from the end. Hydrate measure
  // growth is heightGrew (and often still pinning). First paint has
  // prevHeight 0 — do not treat an unpinned canvas as a leave (🎯T558).
  if (midList && established && !(opts.pinning && opts.heightGrew)) return false;
  if (opts.pinning) return true;
  // Measure growth moves the live end away from the current scrollTop.
  // That looks like a leave if we only read fromBottom (often ⅓–⅔ pane).
  if (opts.wasFollowing && opts.heightGrew) return true;
  const t = opts.threshold == null ? FOLLOW_END_PX : opts.threshold;
  return opts.fromBottom < t;
}
