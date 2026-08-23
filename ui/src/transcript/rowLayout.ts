// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Lifted from web/scripts/virtual_list.js — same extent, not a restyle. */

export const DEFAULT_ROW_GAP_PX = 8;
export const BUBBLE_BOTTOM_CHROME_PX = 19;
/** 12-row pixel fixture is ~9px short of scrolling #messages padding away.
 * Comfortable uses 9 so pin-to-end can sit the first bubble at the pane top. */
export const COMFORTABLE_ROW_GAP_PX = DEFAULT_ROW_GAP_PX + 1;

export type RowLayoutOpts = {
  timeOverflowPx?: number;
  tabOverflowPx?: number;
  marginBottomPx?: number;
  marginTopPx?: number;
};

/**
 * Extent of one row: border-box plus overflowing chrome.
 * Gap between rows is separate (DEFAULT_ROW_GAP_PX / virtualizer gap).
 */
export function rowLayoutHeight(borderBoxHeight: number, opts?: RowLayoutOpts): number {
  const box = Number(borderBoxHeight);
  const h = Number.isFinite(box) && box > 0 ? box : 0;
  const o = opts || {};
  const time = Number(o.timeOverflowPx);
  const overflow = Number.isFinite(time) && time > 0 ? time : 0;
  const tab = Number(o.tabOverflowPx);
  const above = Number.isFinite(tab) && tab > 0 ? tab : 0;
  const mb = Number(o.marginBottomPx);
  const mt = Number(o.marginTopPx);
  const bottom = Number.isFinite(mb) && mb > 0 ? mb : 0;
  const top = Number.isFinite(mt) && mt > 0 ? mt : 0;
  return h + Math.max(overflow, bottom) + Math.max(above, top);
}

/** T351: lock the natural border-box on the pixel grid before chrome. */
export function snappedNaturalBox(borderBoxHeight: number): number {
  const h = Number(borderBoxHeight);
  if (!Number.isFinite(h) || h <= 0) return 0;
  return Math.ceil(h);
}

export function measureTranscriptRow(el: Element): number {
  const box = el.getBoundingClientRect();
  const natural = snappedNaturalBox(box.height);
  const cs = getComputedStyle(el);
  const mt = parseFloat(cs.marginTop) || 0;
  const mb = parseFloat(cs.marginBottom) || 0;
  const kind = el.getAttribute('data-kind');
  let timeOverflow = 0;
  let tabOverflow = 0;
  const extras = el.querySelectorAll('.msg-time, .msg-expand-tab, .msg-context-tab');
  for (let i = 0; i < extras.length; i++) {
    const node = extras[i];
    const ncs = getComputedStyle(node);
    if (ncs.display === 'none') continue;
    const r = node.getBoundingClientRect();
    if (node.classList.contains('msg-time')) {
      timeOverflow = Math.max(timeOverflow, r.bottom - box.bottom);
    } else {
      tabOverflow = Math.max(tabOverflow, box.top - r.top);
    }
  }
  const reserveBottom =
    kind === 'user' || kind === 'assistant' ? Math.max(mb, BUBBLE_BOTTOM_CHROME_PX) : mb;
  return rowLayoutHeight(natural, {
    timeOverflowPx: timeOverflow,
    tabOverflowPx: tabOverflow,
    marginBottomPx: reserveBottom,
    marginTopPx: mt,
  });
}
