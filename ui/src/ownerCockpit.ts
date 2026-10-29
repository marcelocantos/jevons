// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Bootstrap the HttpOnly owner cockpit session cookie (🎯T1129). */
export async function bootstrapOwnerCockpitSession(): Promise<void> {
  try {
    const res = await fetch('/api/owner-cockpit/session', { method: 'GET', credentials: 'same-origin' });
    if (!res.ok && res.status !== 204) {
      console.warn('owner cockpit session bootstrap failed', res.status);
    }
  } catch (err) {
    console.warn('owner cockpit session bootstrap error', err);
  }
}
