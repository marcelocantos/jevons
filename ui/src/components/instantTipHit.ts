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

export type CardPlacement = 'left-of-host' | 'right-of-host';

export type PlaceCardResult = {
  left: number;
  top: number;
  side: 'left' | 'right';
  maxWidth?: number;
};

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
  let tw = Math.max(0, Number(args.tipW) || 0);
  const th = Math.max(0, Number(args.tipH) || 0);
  const vw = Math.max(0, Number(args.viewW) || 0);
  const vh = Math.max(0, Number(args.viewH) || 0);
  const hx = host ? host.left : 0;
  const hy = host ? (host.top + host.bottom) / 2 : 0;
  const placement = args.placement || 'left-of-host';

  if (placement === 'right-of-host') {
    let side: 'left' | 'right' = 'right';
    let left = (host ? host.right : hx) + gap;
    if (vw > 0 && left + tw > vw - pad) {
      const hostLeft = host ? host.left : hx;
      const flip = hostLeft - gap - tw;
      if (flip >= pad) {
        side = 'left';
        left = flip;
      } else {
        left = Math.max(pad, vw - pad - tw);
      }
    }
    if (left < pad) left = pad;
    let top = hy - th / 2;
    if (vh > 0) {
      if (top + th > vh - pad) top = Math.max(pad, vh - pad - th);
      if (top < pad) top = pad;
    }
    return { left: Math.round(left), top: Math.round(top), side };
  }

  let side: 'left' | 'right' = 'left';
  let left = hx - gap - tw;
  let maxWidth: number | undefined;
  const maxRight = args.clampRight != null && Number.isFinite(args.clampRight) ? Number(args.clampRight) : null;

  if (maxRight != null) {
    if (left + tw > maxRight) left = maxRight - tw;
    if (left < pad) {
      left = pad;
      const avail = Math.max(0, maxRight - pad);
      if (avail > 0 && (tw <= 0 || avail < tw)) {
        maxWidth = Math.floor(avail);
        tw = maxWidth;
        left = maxRight - tw;
        if (left < pad) left = pad;
      }
    }
  } else if (left < pad) {
    side = 'right';
    left = (host ? host.right : hx) + gap;
  }
  if (vw > 0) {
    if (left + tw > vw - pad) left = Math.max(pad, vw - pad - tw);
    if (left < pad) left = pad;
  }

  let top = hy - th / 2;
  if (vh > 0) {
    if (top + th > vh - pad) top = Math.max(pad, vh - pad - th);
    if (top < pad) top = pad;
  } else if (top < pad) {
    top = pad;
  }

  const out: PlaceCardResult = { left: Math.round(left), top: Math.round(top), side };
  if (maxWidth != null) out.maxWidth = maxWidth;
  return out;
}
