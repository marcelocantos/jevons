// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T91: absolute local time for the hover title; relTime stays in chrome.
 * Lifted from web/index.html absTimeTitle — one clock. */
export function absTimeTitle(ms: number): string {
  try {
    return new Date(ms).toLocaleString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch {
    return String(ms);
  }
}
