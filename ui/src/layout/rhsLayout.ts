// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Lifted from web/scripts/rhs_layout.js. Same localStorage key as the old app. */

export const STORAGE_KEY = 'jevons-rhs-layout-v1';
export const DEFAULT_SIDEBAR_WIDTH = 420;
export const DEFAULT_FLEET_FRACTION = 0.45;
export const MIN_SIDEBAR_WIDTH = 220;
export const MIN_CHAT_WIDTH = 280;
export const MIN_FLEET_PX = 60;
export const MIN_BOTTOM_PX = 140;
export const SPLIT_HANDLE_PX = 6;

export type RhsLayoutState = {
  sidebarWidth: number;
  fleetFraction: number;
};

function num(v: unknown, fallback: number): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : fallback;
}

function clamp(n: number, lo: number, hi: number): number {
  if (!(hi >= lo)) return lo;
  return Math.min(Math.max(n, lo), hi);
}

export function clampSidebarWidth(requested: number, mainWidth: number): number {
  const main = Math.max(0, num(mainWidth, 0));
  const maxSide = Math.max(MIN_SIDEBAR_WIDTH, main - MIN_CHAT_WIDTH);
  return clamp(num(requested, DEFAULT_SIDEBAR_WIDTH), MIN_SIDEBAR_WIDTH, maxSide);
}

export function clampFleetFraction(fraction: number, splitHeight: number): number {
  const splitH = Math.max(0, num(splitHeight, 0));
  const usable = Math.max(0, splitH - SPLIT_HANDLE_PX);
  if (!(usable > 0)) {
    return clamp(num(fraction, DEFAULT_FLEET_FRACTION), 0, 1);
  }
  if (usable < MIN_FLEET_PX + MIN_BOTTOM_PX) {
    return clamp(MIN_FLEET_PX / Math.max(1, MIN_FLEET_PX + MIN_BOTTOM_PX), 0, 1);
  }
  const minF = MIN_FLEET_PX / usable;
  const maxF = 1 - MIN_BOTTOM_PX / usable;
  return clamp(num(fraction, DEFAULT_FLEET_FRACTION), minF, maxF);
}

export function fleetFractionFromPointer(pointerYInSplit: number, splitHeight: number): number {
  const usable = Math.max(1, Math.max(0, num(splitHeight, 0)) - SPLIT_HANDLE_PX);
  const y = Math.max(0, num(pointerYInSplit, 0));
  return clampFleetFraction(y / usable, splitHeight);
}

export function sidebarWidthFromPointer(pointerXInMain: number, mainWidth: number): number {
  const main = Math.max(0, num(mainWidth, 0));
  const x = num(pointerXInMain, main - DEFAULT_SIDEBAR_WIDTH);
  return clampSidebarWidth(main - x, main);
}

export function defaultState(): RhsLayoutState {
  return { sidebarWidth: DEFAULT_SIDEBAR_WIDTH, fleetFraction: DEFAULT_FLEET_FRACTION };
}

export function normalizeState(raw: unknown): RhsLayoutState {
  const d = defaultState();
  if (!raw || typeof raw !== 'object') return d;
  const rec = raw as Record<string, unknown>;
  const w = num(rec.sidebarWidth, d.sidebarWidth);
  const f = num(rec.fleetFraction, d.fleetFraction);
  return {
    sidebarWidth: w > 0 ? w : d.sidebarWidth,
    fleetFraction: f > 0 && f <= 1 ? f : d.fleetFraction,
  };
}

export function serialize(state: RhsLayoutState): string {
  const s = normalizeState(state);
  return JSON.stringify({ sidebarWidth: s.sidebarWidth, fleetFraction: s.fleetFraction });
}

export function deserialize(raw: string | null | undefined): {
  ok: boolean;
  state: RhsLayoutState;
  present: boolean;
} {
  if (raw == null || raw === '') {
    return { ok: true, state: defaultState(), present: false };
  }
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') {
      return { ok: false, state: defaultState(), present: true };
    }
    return { ok: true, state: normalizeState(parsed), present: true };
  } catch {
    return { ok: false, state: defaultState(), present: true };
  }
}

export function load(storage: Pick<Storage, 'getItem'> | null | undefined): {
  ok: boolean;
  state: RhsLayoutState;
  present: boolean;
} {
  if (!storage || typeof storage.getItem !== 'function') {
    return { ok: true, state: defaultState(), present: false };
  }
  try {
    return deserialize(storage.getItem(STORAGE_KEY));
  } catch {
    return { ok: false, state: defaultState(), present: false };
  }
}

export function save(
  storage: Pick<Storage, 'setItem'> | null | undefined,
  state: RhsLayoutState,
): { ok: boolean } {
  if (!storage || typeof storage.setItem !== 'function') return { ok: false };
  try {
    storage.setItem(STORAGE_KEY, serialize(state));
    return { ok: true };
  } catch {
    return { ok: false };
  }
}

export function stylesForState(state: RhsLayoutState): {
  sidebarWidthPx: number;
  fleetFlexBasis: string;
  fleetFraction: number;
} {
  const s = normalizeState(state);
  const pct = Math.round(s.fleetFraction * 10000) / 100;
  return {
    sidebarWidthPx: Math.round(s.sidebarWidth),
    fleetFlexBasis: pct + '%',
    fleetFraction: s.fleetFraction,
  };
}
