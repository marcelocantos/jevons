// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/**
 * Plan-usage tooltip as a table (🎯T588.1).
 *
 * The prose form put five providers on five lines and made the owner read
 * a sentence to compare two numbers. A grid puts each measure on its own
 * row, so "which provider has least left" is a glance along one row rather
 * than a parse of five sentences.
 */

import { now } from '../clock';
import { formatWindow } from './pace';
import { CompanyMark, companyOfProvider } from './companyMark';
import { formatInstantParts } from './tickerGroups';
import type { PlanWindow, TickerGroup } from './tickerGroups';
import { BurnChart } from './BurnChart';

const SECONDS_PER_MINUTE = 60;
const MINUTES_PER_HOUR = 60;
const HOURS_PER_DAY = 24;
const DAYS_PER_WEEK = 7;

/** One header column: a provider's window, or the provider itself when alone. */
export type TipColumn = {
  provider: string;
  /** Window label under the provider icon: 'session', 'week', 'month'. */
  label: string;
  window: PlanWindow;
};

/** Header grouping: a provider and the columns it spans. */
export type TipHeaderGroup = { provider: string; columns: TipColumn[] };

/** week reads better than weekly in a two-row header; the row is narrow. */
export function windowLabel(name: string): string {
  const n = String(name || '').toLowerCase();
  if (n === 'weekly') return 'week';
  if (n === 'monthly') return 'month';
  return n || 'window';
}

/**
 * tipColumns flattens available groups into columns, preserving provider
 * order. A provider with no windows contributes none — the table shows
 * only what the feed actually published (🎯T390).
 */
export function tipColumns(groups: TickerGroup[]): TipHeaderGroup[] {
  const out: TipHeaderGroup[] = [];
  for (const g of groups) {
    if (!g.available) continue;
    const columns = (g.windows || []).map((w) => ({
      provider: g.provider,
      label: windowLabel(w.name || ''),
      window: w,
    }));
    if (columns.length) out.push({ provider: g.provider, columns });
  }
  return out;
}

/** A percentage the feed published, or '—'. Never an invented number. */
export function pct(v: number | null | undefined): string {
  return typeof v === 'number' ? `${Math.round(v)}%` : '—';
}

/**
 * humanDuration renders a span the way the owner would say it: '2d 4h',
 * '1h37m', '12m', 'now'. Deliberately coarse — a rollover three hours out
 * is no more useful with seconds on it. Mirrors humanDuration in the
 * vanilla web/scripts/plan_usage.js.
 */
export function humanDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  if (s < SECONDS_PER_MINUTE) return 'now';
  const totalMinutes = Math.floor(s / SECONDS_PER_MINUTE);
  const minutes = totalMinutes % MINUTES_PER_HOUR;
  const totalHours = Math.floor(totalMinutes / MINUTES_PER_HOUR);
  const hours = totalHours % HOURS_PER_DAY;
  const days = Math.floor(totalHours / HOURS_PER_DAY);
  if (days > 0) return days + 'd' + (hours > 0 ? ` ${hours}h` : '');
  if (totalHours > 0) return totalHours + 'h' + (minutes > 0 ? String(minutes).padStart(2, '0') + 'm' : '');
  return totalMinutes + 'm';
}

/** Time until the window rolls over, or '—' when it publishes no instant. */
export function timeLeft(w: PlanWindow, nowMs: number): string {
  if (!w.resets_at) return '—';
  const at = new Date(w.resets_at);
  if (Number.isNaN(at.getTime())) return '—';
  return humanDuration((at.getTime() - nowMs) / 1000);
}

/**
 * rolloverCell names the moment in the owner's own zone (🎯T588).
 *
 * Inside a week the weekday is the useful handle — 'Tue 14:20' answers
 * "when" without arithmetic. Past a week a weekday is ambiguous (which
 * Tuesday?), so the date replaces it. Codex is the only backend far
 * enough out to show it today, which is exactly why the rule is written
 * from the data rather than from the current fleet.
 */
export function rolloverCell(
  iso: string | null | undefined,
  nowMs: number,
  timeZone?: string,
): string {
  if (!iso) return '—';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '—';
  const beyondAWeek =
    at.getTime() - nowMs >= DAYS_PER_WEEK * HOURS_PER_DAY * MINUTES_PER_HOUR * SECONDS_PER_MINUTE * 1000;
  const opts: Intl.DateTimeFormatOptions = beyondAWeek
    ? { timeZone, day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false }
    : { timeZone, weekday: 'short', hour: '2-digit', minute: '2-digit', hour12: false };
  try {
    return formatInstantParts(at, opts);
  } catch {
    return '—';
  }
}

/** Providers the feed could not answer for; shown under the table, not dropped. */
export function unavailableNotes(groups: TickerGroup[]): string[] {
  return groups
    .filter((g) => !g.available)
    .map((g) => `${g.provider}: unavailable — ${g.reason || 'no plan-remaining published'}`);
}

export function PlanTipTable(props: { groups: TickerGroup[]; nowMs?: number; timeZone?: string }) {
  const nowMs = props.nowMs ?? now();
  const header = tipColumns(props.groups);
  const notes = unavailableNotes(props.groups);
  if (!header.length) {
    return <div className="plan-tip-empty">{notes.join('\n') || 'Plan remaining unavailable'}</div>;
  }
  const cols = header.flatMap((h) => h.columns);
  const multi = (h: TipHeaderGroup) => h.columns.length > 1;
  // The available figure carries the same pace class the bar's fill does,
  // so the number and the bar above it say the same thing in the same
  // colour (🎯T588.2). Reusing formatWindow rather than re-deriving the
  // class is the point: two sources would drift and the tooltip would
  // quietly disagree with the bar it describes.
  const paceClass = (c: TipColumn) => formatWindow(c.window, nowMs).className || '';
  const row = (
    label: string,
    cell: (c: TipColumn) => string,
    cls?: (c: TipColumn) => string,
  ) => (
    <tr>
      <th scope="row">{label}</th>
      {cols.map((c) => (
        <td
          key={`${c.provider}:${c.label}`}
          className={cls ? cls(c) : undefined}
          data-provider={c.provider}
          data-window={c.label}
        >
          {cell(c)}
        </td>
      ))}
    </tr>
  );
  return (
    <div className="plan-tip">
      <table className="plan-tip-table">
        <thead>
          <tr>
            <td />
            {header.map((h) => (
              <th
                key={h.provider}
                scope="col"
                colSpan={h.columns.length}
                rowSpan={multi(h) ? 1 : 2}
                data-provider={h.provider}
                title={h.provider}
              >
                <CompanyMark provider={h.provider} company={companyOfProvider(h.provider)} />
              </th>
            ))}
          </tr>
          <tr>
            <td />
            {header.filter(multi).flatMap((h) =>
              h.columns.map((c) => (
                <th key={`${c.provider}:${c.label}`} scope="col" className="plan-tip-win">
                  {c.label}
                </th>
              )),
            )}
          </tr>
        </thead>
        <tbody>
          {row(
            'available',
            (c) => pct(c.window.remaining_percent),
            (c) => ('plan-avail ' + paceClass(c)).trim(),
          )}
          {row('time left', (c) => timeLeft(c.window, nowMs))}
          {row('consumed', (c) => pct(c.window.used_percent))}
          {row('rollover', (c) => rolloverCell(c.window.resets_at, nowMs, props.timeZone))}
          <tr>
            <th scope="row">burn</th>
            {cols.map((c) => (
              <td
                key={`${c.provider}:${c.label}`}
                className={('plan-burn ' + paceClass(c)).trim()}
                data-provider={c.provider}
                data-window={c.label}
              >
                <BurnChart window={c.window} />
              </td>
            ))}
          </tr>
        </tbody>
      </table>
      {notes.length ? <div className="plan-tip-notes">{notes.join('\n')}</div> : null}
    </div>
  );
}
