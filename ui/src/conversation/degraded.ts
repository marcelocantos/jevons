// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Standing overseer-down banner from a mux meta / status sample (🎯T545.6). */

export function degradedBannerText(body: unknown): string {
  const o = body && typeof body === 'object' ? (body as Record<string, unknown>) : {};
  const down = String(o.overseer_down ?? '').trim();
  if (!down) return '';
  if (/^cockpit degraded:/i.test(down)) return down;
  return 'Cockpit degraded: ' + down;
}
