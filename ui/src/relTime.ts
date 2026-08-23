// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { date, now } from './clock';

/** Lifted from web/index.html relTime — one clock. */
export function relTime(ms: number): string {
  const s = Math.floor((now() - ms) / 1000);
  if (s < 60) return 'now';
  const m = Math.floor(s / 60);
  if (m < 60) return m + 'm';
  const h = Math.floor(m / 60);
  if (h < 24) return h + 'h';
  const d = Math.floor(h / 24);
  if (d < 7) return d + 'd';
  const dt = new Date(ms);
  const n = date();
  if (dt.getFullYear() === n.getFullYear()) {
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    if (d < 14) {
      return (
        days[dt.getDay()] +
        ' ' +
        String(dt.getHours()).padStart(2, '0') +
        ':' +
        String(dt.getMinutes()).padStart(2, '0')
      );
    }
    return months[dt.getMonth()] + ' ' + dt.getDate();
  }
  return dt.toLocaleDateString();
}
