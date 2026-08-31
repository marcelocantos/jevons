// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Distance from the live end. */
export function distanceFromEnd(
  scrollTop: number,
  scrollHeight: number,
  clientHeight: number,
): number {
  return Math.max(
    0,
    (Number(scrollHeight) || 0) -
      (Number(scrollTop) || 0) -
      (Number(clientHeight) || 0),
  );
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
 * Slack for a scroller the pin has just written. 🎯T351 over-assigns
 * scrollHeight and lets the browser clamp, so a following scroller sits a
 * fraction of a pixel from the end; a row re-measuring adds a few more.
 * This is NOT a "close enough to the end" band — that is FOLLOW_END_PX,
 * and it only decides re-attach. Keeping the two apart is the whole fix:
 * a leave must be judged against what the user did, not against a
 * distance that content growth can manufacture on its own.
 */
export const PIN_RESIDUAL_PX = 8;

/**
 * growthSince is how much canvas appeared below the viewport since the
 * last measurement — a long agent report, a fold expanding, twenty
 * estimated rows re-measuring taller than the 72px guess.
 */
function growthSince(scrollHeight: number, prevHeight: number): number {
  return Math.max(0, (Number(scrollHeight) || 0) - (Number(prevHeight) || 0));
}

/**
 * explainedByGrowth reports whether the distance from the end is simply
 * the content that arrived under the viewport.
 *
 * This is the discriminator the earlier rules were missing. Appending a
 * message taller than the pane moves the live end away by exactly its
 * height while scrollTop never changes — indistinguishable, if you only
 * read fromBottom, from the owner scrolling up by that much. Both read as
 * "more than one viewport from the end", so 🎯T556's mid-list guard
 * detached on a burst of worker reports and left the transcript parked a
 * page or two up, every time, with no scroll and no way back but Latest
 * (🎯T587). A real leave moves scrollTop while the canvas stands still,
 * so growth cannot account for it.
 */
export function explainedByGrowth(o: {
  fromBottom: number;
  scrollHeight: number;
  prevHeight: number;
}): boolean {
  const prev = Number(o.prevHeight) || 0;
  if (prev <= 0) return false; // no baseline yet — caller decides (🎯T558)
  return (
    (Number(o.fromBottom) || 0) <=
    growthSince(o.scrollHeight, prev) + PIN_RESIDUAL_PX
  );
}

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
      scrollHeight: sh,
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
  scrollHeight?: number;
  prevHeight?: number;
}): boolean {
  if ((Number(opts.scrollTop) || 0) <= 0) return false;
  const ch = Number(opts.clientHeight) || 0;
  if (ch <= 0) return false;
  if ((Number(opts.fromBottom) || 0) <= ch) return false;
  // Past a viewport from the end — but content growing under a pinned
  // scroller looks exactly like that, and abandoning the pin there is
  // what parks the transcript mid-history (🎯T587).
  if (
    explainedByGrowth({
      fromBottom: opts.fromBottom,
      scrollHeight: Number(opts.scrollHeight) || 0,
      prevHeight: Number(opts.prevHeight) || 0,
    })
  ) {
    return false;
  }
  return true;
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
  /** Current canvas height, so growth since prevHeight is knowable. */
  scrollHeight?: number;
}): boolean {
  const from = Number(opts.fromBottom) || 0;
  const ch = Number(opts.clientHeight) || 0;
  const midList = ch > 0 && from > ch;
  const established = (Number(opts.prevHeight) || 0) > 0;
  // Distance the canvas grew into is not distance the user travelled
  // (🎯T587). Checked before the mid-list rule, which cannot tell them
  // apart on fromBottom alone.
  if (
    established &&
    explainedByGrowth({
      fromBottom: from,
      scrollHeight: Number(opts.scrollHeight) || 0,
      prevHeight: Number(opts.prevHeight) || 0,
    })
  ) {
    return true;
  }
  // User moved more than one viewport from the end. Hydrate measure
  // growth is heightGrew (and often still pinning). First paint has
  // prevHeight 0 — do not treat an unpinned canvas as a leave (🎯T558).
  if (midList && established && !(opts.pinning && opts.heightGrew))
    return false;
  if (opts.pinning) return true;
  // Measure growth moves the live end away from the current scrollTop.
  // That looks like a leave if we only read fromBottom (often ⅓–⅔ pane).
  if (opts.wasFollowing && opts.heightGrew) return true;
  const t = opts.threshold == null ? FOLLOW_END_PX : opts.threshold;
  return opts.fromBottom < t;
}
