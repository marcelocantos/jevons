// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Harnessable wall clock. Lifted into ui/; do not import web/scripts/clock.js. */

let frozen: number | null = readInitFreeze();

function readInitFreeze(): number | null {
  const g = globalThis as { __JEVONS_CLOCK_NOW?: unknown };
  if (g.__JEVONS_CLOCK_NOW == null || g.__JEVONS_CLOCK_NOW === false) return null;
  const n = Number(g.__JEVONS_CLOCK_NOW);
  return Number.isFinite(n) ? n : null;
}

export function now(): number {
  return frozen != null ? frozen : Date.now();
}

export function date(): Date {
  return new Date(now());
}

export function setNow(ms: number | null | false): void {
  if (ms == null || ms === false) {
    frozen = null;
    return;
  }
  const n = Number(ms);
  frozen = Number.isFinite(n) ? n : null;
}

export function reset(): void {
  frozen = null;
}

export function isFrozen(): boolean {
  return frozen != null;
}
