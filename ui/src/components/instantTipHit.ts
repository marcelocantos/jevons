// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T231 / T271 InstantTip hit geometry. Ported from web/scripts/instant_tip.js. */

export type HitRect = { left: number; top: number; right: number; bottom: number };

/** Product path: leave the hit region → dismiss. Flicker → fix geometry, never a timeout. */
export const HIDE_GRACE_MS = 0;

export function normalizeRect(r: Partial<HitRect> & { width?: number; height?: number } | null | undefined): HitRect | null {
  if (!r || typeof r !== 'object') return null;
  const left = Number(r.left);
  const top = Number(r.top);
  if (!Number.isFinite(left) || !Number.isFinite(top)) return null;
  const right =
    r.right != null && Number.isFinite(Number(r.right))
      ? Number(r.right)
      : left + (Number(r.width) || 0);
  const bottom =
    r.bottom != null && Number.isFinite(Number(r.bottom))
      ? Number(r.bottom)
      : top + (Number(r.height) || 0);
  return { left, top, right, bottom };
}

export function unionHitRect(rects: Array<Partial<HitRect> | null | undefined>): HitRect | null {
  const list = (rects || []).map(normalizeRect).filter((x): x is HitRect => !!x);
  if (!list.length) return null;
  let { left, top, right, bottom } = list[0];
  for (let i = 1; i < list.length; i++) {
    left = Math.min(left, list[i].left);
    top = Math.min(top, list[i].top);
    right = Math.max(right, list[i].right);
    bottom = Math.max(bottom, list[i].bottom);
  }
  return { left, top, right, bottom };
}

export function pointInHitRect(x: number, y: number, rect: Partial<HitRect> | null | undefined): boolean {
  const r = normalizeRect(rect);
  if (!r) return false;
  const px = Number(x);
  const py = Number(y);
  if (!Number.isFinite(px) || !Number.isFinite(py)) return false;
  return px >= r.left && px <= r.right && py >= r.top && py <= r.bottom;
}

/** Horizontal corridor between card and hosts (🎯T271). Not a tall AABB over other rows. */
export function bridgeCorridorBetween(
  cardRect: Partial<HitRect> | null | undefined,
  hostRects: Array<Partial<HitRect> | null | undefined> | Partial<HitRect> | null,
): HitRect | null {
  const card = normalizeRect(cardRect);
  const hostsUnion = Array.isArray(hostRects) ? unionHitRect(hostRects) : normalizeRect(hostRects);
  if (!card || !hostsUnion) return null;
  const top = Math.min(card.top, hostsUnion.top);
  const bottom = Math.max(card.bottom, hostsUnion.bottom);
  let left: number;
  let right: number;
  if (card.right <= hostsUnion.left) {
    left = card.right;
    right = hostsUnion.left;
  } else if (hostsUnion.right <= card.left) {
    left = hostsUnion.right;
    right = card.left;
  } else {
    return null;
  }
  if (!(right > left) || !(bottom > top)) return null;
  return { left, top, right, bottom };
}

export type HitParts = {
  card: HitRect | null;
  hosts: HitRect[];
  corridor: HitRect | null;
  aabb: HitRect | null;
};

export function computeHitParts(args: {
  cardRect?: Partial<HitRect> | null;
  tipRect?: Partial<HitRect> | null;
  hostRects?: Array<Partial<HitRect> | null | undefined>;
}): HitParts {
  const card = normalizeRect(args.cardRect || args.tipRect);
  const hosts: HitRect[] = [];
  for (const raw of args.hostRects || []) {
    const h = normalizeRect(raw);
    if (h) hosts.push(h);
  }
  const corridor = bridgeCorridorBetween(card, hosts);
  const envelope: HitRect[] = [];
  if (card) envelope.push(card);
  envelope.push(...hosts);
  if (corridor) envelope.push(corridor);
  return { card, hosts, corridor, aabb: unionHitRect(envelope) };
}

export function pointInHitParts(
  x: number,
  y: number,
  parts: { card?: HitRect | null; hosts?: HitRect[]; corridor?: HitRect | null; rect?: HitRect | null },
): boolean {
  if (parts.rect) return pointInHitRect(x, y, parts.rect);
  if (pointInHitRect(x, y, parts.card)) return true;
  for (const h of parts.hosts || []) {
    if (pointInHitRect(x, y, h)) return true;
  }
  if (parts.corridor && pointInHitRect(x, y, parts.corridor)) return true;
  return false;
}

export function shouldDismissOutsideHitParts(
  x: number,
  y: number,
  parts: Parameters<typeof pointInHitParts>[2],
): boolean {
  return !pointInHitParts(x, y, parts);
}

export function samePointerSample(
  prev: { x: number; y: number } | null | undefined,
  x: number,
  y: number,
): boolean {
  return !!prev && prev.x === x && prev.y === y;
}

export function hitRectIsDegenerate(r: HitRect | null | undefined): boolean {
  if (!r) return true;
  return r.right - r.left < 8 || r.bottom - r.top < 8;
}

/** 🎯T648: layout/mermaid resize is not a leave. Same clientXY is not a leave. */
export function shouldDismissPointerSample(args: {
  x: number;
  y: number;
  lastXY?: { x: number; y: number } | null;
  parts: HitParts;
  lastParts?: HitParts | null;
}): boolean {
  if (samePointerSample(args.lastXY, args.x, args.y)) return false;
  const parts =
    hitRectIsDegenerate(args.parts.card) && args.lastParts ? args.lastParts : args.parts;
  return shouldDismissOutsideHitParts(args.x, args.y, parts);
}

export function stickCardRect(args: {
  left: number;
  top: number;
  /** Host-facing side from the first place. Left-of-host cards grow left. */
  side?: 'left' | 'right';
  /** Width at the last place — used to keep the host-facing edge still. */
  prevW?: number;
  tipW: number;
  tipH: number;
  viewW: number;
  viewH: number;
  pad?: number;
  clampRight?: number | null;
}): { left: number; top: number; maxWidth?: number } {
  const pad = args.pad != null ? args.pad : 8;
  let tw = Math.max(0, Number(args.tipW) || 0);
  const th = Math.max(0, Number(args.tipH) || 0);
  const vw = Math.max(0, Number(args.viewW) || 0);
  const vh = Math.max(0, Number(args.viewH) || 0);
  const prevW = args.prevW != null && args.prevW > 0 ? args.prevW : tw;
  let maxWidth: number | undefined;
  let left = args.left;

  if (args.side === 'left') {
    // Pin the host-facing (right) edge so growth expands away from the trigger.
    let pinnedRight = args.left + prevW;
    if (args.clampRight != null && Number.isFinite(args.clampRight)) {
      pinnedRight = Math.min(pinnedRight, Number(args.clampRight));
    }
    left = pinnedRight - tw;
    if (left < pad) {
      const avail = Math.max(0, pinnedRight - pad);
      if (avail < tw) {
        maxWidth = Math.floor(avail);
        tw = maxWidth;
      }
      left = pinnedRight - tw;
    }
  } else {
    if (args.clampRight != null && Number.isFinite(args.clampRight) && left + tw > args.clampRight) {
      const avail = Math.max(0, Number(args.clampRight) - left);
      if (avail < tw) {
        maxWidth = Math.floor(avail);
        tw = maxWidth;
      }
    }
    if (vw > 0 && left + tw > vw - pad) {
      const avail = Math.max(0, vw - pad - left);
      if (avail < tw) {
        maxWidth = Math.floor(avail);
        tw = maxWidth;
      }
    }
  }

  let top = args.top;
  if (vh > 0 && top + th > vh - pad) top = Math.max(pad, vh - pad - th);
  if (top < pad) top = pad;
  const out: { left: number; top: number; maxWidth?: number } = {
    left: Math.round(left),
    top: Math.round(top),
  };
  if (maxWidth != null) out.maxWidth = maxWidth;
  return out;
}

export type CardPlacement = 'left-of-host' | 'right-of-host' | 'toward-mid' | 'below-host';

export type PlaceCardResult = {
  left: number;
  top: number;
  side: 'left' | 'right';
  maxWidth?: number;
};

/** Card sits on the trigger — the pointer cannot walk adjacent hosts (T181/T186/T326/T648). */
export function cardCoversHost(
  card: { left: number; width?: number; right?: number },
  host: Partial<HitRect>,
  gap = 8,
): boolean {
  const h = normalizeRect(host);
  if (!h) return false;
  const right = card.right != null ? Number(card.right) : card.left + (Number(card.width) || 0);
  if (!Number.isFinite(card.left) || !Number.isFinite(right)) return false;
  return card.left < h.right + gap && right > h.left - gap;
}

/** Transcript: open on the side that puts the card closer to the pane middle. */
export function sideTowardMid(host: Partial<HitRect>, viewW: number): 'left' | 'right' {
  const h = normalizeRect(host);
  const hx = h ? (h.left + h.right) / 2 : 0;
  const mid = viewW / 2;
  return hx <= mid ? 'right' : 'left';
}

function placeVertical(hy: number, th: number, vh: number, pad: number): number {
  let top = hy - th / 2;
  if (vh > 0) {
    if (top + th > vh - pad) top = Math.max(pad, vh - pad - th);
    if (top < pad) top = pad;
  } else if (top < pad) {
    top = pad;
  }
  return Math.round(top);
}

/** Sit entirely on one side of the host. Never overlay. Shrink if the gutter is tight. */
function placeBesideHost(args: {
  side: 'left' | 'right';
  host: HitRect | null;
  hx: number;
  hy: number;
  tw: number;
  th: number;
  vw: number;
  vh: number;
  pad: number;
  gap: number;
  clampRight?: number | null;
}): PlaceCardResult {
  const host = args.host;
  let tw = args.tw;
  let maxWidth: number | undefined;
  let left: number;

  if (args.side === 'left') {
    const hostClamp = (host ? host.left : args.hx) - args.gap;
    const maxRight =
      args.clampRight != null && Number.isFinite(args.clampRight)
        ? Math.min(Number(args.clampRight), hostClamp)
        : hostClamp;
    left = maxRight - tw;
    if (left < args.pad) {
      const avail = Math.max(0, maxRight - args.pad);
      if (avail < tw) {
        maxWidth = Math.floor(avail);
        tw = maxWidth;
      }
      left = maxRight - tw;
      if (left < args.pad) left = args.pad;
    }
  } else {
    const minLeft = (host ? host.right : args.hx) + args.gap;
    left = minLeft;
    if (args.vw > 0 && left + tw > args.vw - args.pad) {
      const avail = Math.max(0, args.vw - args.pad - minLeft);
      if (avail < tw) {
        maxWidth = Math.floor(avail);
        tw = maxWidth;
      }
      left = minLeft;
    }
    if (left < args.pad) left = args.pad;
  }

  const out: PlaceCardResult = {
    left: Math.round(left),
    top: placeVertical(args.hy, args.th, args.vh, args.pad),
    side: args.side,
  };
  if (maxWidth != null) out.maxWidth = maxWidth;
  return out;
}

/** Vanilla T181/T186: left of host, right edge clamped off #frontier-table; top centered. */
export function placeCardRect(args: {
  placement?: CardPlacement;
  host: Partial<HitRect>;
  tipW: number;
  tipH: number;
  viewW: number;
  viewH: number;
  clampRight?: number | null;
  pad?: number;
  gap?: number;
}): PlaceCardResult {
  const host = normalizeRect(args.host);
  const pad = args.pad != null ? args.pad : 8;
  const gap = args.gap != null ? args.gap : 8;
  const tw = Math.max(0, Number(args.tipW) || 0);
  const th = Math.max(0, Number(args.tipH) || 0);
  const vw = Math.max(0, Number(args.viewW) || 0);
  const vh = Math.max(0, Number(args.viewH) || 0);
  const hx = host ? host.left : 0;
  const hy = host ? (host.top + host.bottom) / 2 : 0;
  const placement = args.placement || 'left-of-host';

  if (placement === 'below-host') {
    // Under the host, right edges flush (🎯T588.2). The plan tip is a grid
    // whose columns line up with the bars it describes, so hanging it off
    // to one side breaks the correspondence the owner is reading.
    let left = (host ? host.right : hx) - tw;
    let top = (host ? host.bottom : hy) + gap;
    if (vw > 0 && left + tw > vw - pad) left = vw - pad - tw;
    if (left < pad) left = pad;
    if (vh > 0 && top + th > vh - pad) {
      // No room below: sit above rather than run off the bottom.
      const above = (host ? host.top : hy) - gap - th;
      top = above >= pad ? above : Math.max(pad, vh - pad - th);
    }
    if (top < pad) top = pad;
    return { left: Math.round(left), top: Math.round(top), side: 'left' };
  }

  const beside = (side: 'left' | 'right') =>
    placeBesideHost({
      side,
      host,
      hx,
      hy,
      tw,
      th,
      vw,
      vh,
      pad,
      gap,
      clampRight: args.clampRight,
    });

  if (placement === 'right-of-host') {
    const right = beside('right');
    if (right.maxWidth != null && right.maxWidth < tw) {
      const left = beside('left');
      if (left.maxWidth == null || (left.maxWidth ?? 0) >= (right.maxWidth ?? 0)) return left;
    }
    return right;
  }

  if (placement === 'toward-mid') {
    const prefer = sideTowardMid(host || { left: hx, top: 0, right: hx, bottom: 0 }, vw);
    const first = beside(prefer);
    if (first.maxWidth != null && first.maxWidth < tw) {
      const alt = beside(prefer === 'left' ? 'right' : 'left');
      if (alt.maxWidth == null || (alt.maxWidth ?? 0) > (first.maxWidth ?? 0)) return alt;
    }
    return first;
  }

  // left-of-host (frontier): never flip over the ID column (T186 / T648).
  return beside('left');
}
