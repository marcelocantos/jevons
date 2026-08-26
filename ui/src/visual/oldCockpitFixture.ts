// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Frozen replay of old-cockpit-1440x900.png. Clock must be PIXEL_FIXTURE_NOW. */

export const PIXEL_FIXTURE_NOW = 1_700_000_000_000;

function agoHours(h: number): number {
  return PIXEL_FIXTURE_NOW - h * 3600 * 1000;
}

function notes(n: number, when: number): unknown[] {
  const out: unknown[] = [];
  for (let i = 0; i < n; i++) {
    out.push({ type: 'agent_note', text: 'step ' + (i + 1), when });
  }
  return out;
}

function assistant(text: string, when: number): unknown {
  return { type: 'assistant', when, message: { content: [{ type: 'text', text }] } };
}

export function pixelFixtureFrames(): unknown[] {
  const t18 = agoHours(18);
  const t10 = agoHours(10);
  const t9 = agoHours(9);
  const t5a = agoHours(5);
  const t5b = agoHours(5) - 60_000;
  const t4 = agoHours(4);
  return [
    assistant(
      'That order is already done. After this bounce the fleet is still only **jevons** and **jevons-po**. No work agents came back.\nThe freeze is still on, so nothing will remint.',
      t18,
    ),
    ...notes(5, t18),
    assistant(
      "I'll verify the fleet is still just you and the PO, and kill anyone who came back.\n\nThat exit order is complete. Fleet is still only **jevons** and **jevons-po**. Freeze is still on, so nothing remints.\n\nThis resume event is replaying a finished instruction. I am not killing or spawning anyone.",
      t10,
    ),
    ...notes(6, t10),
    assistant(
      'The coach flagged 23 kill failures. I\'ll read the eventlog and decide whether that is a real gap or noise from the freeze/exit.',
      t9,
    ),
    ...notes(8, t9),
    assistant(
      'The jevons freeze is already on, and the fleet is still only you and the PO. This load spike is not us.\n\nWhat is burning the machine: **Ghostty, sawmill** (~4 GB), and **mnemo**. I am not touching those.',
      t5a,
    ),
    ...notes(2, t5a),
    assistant(
      'The cockpit heartbeat looks stale. I\'ll check the daily seat, then reply on that path.\n\nDaily seat is up (**`/health` 200**). The stale heartbeat is the tab, not a dead daemon — I am on this path.\n\nFreeze still holds. Fleet is still only jevons and jevons-po. Hard-reload if chrome looks wedged.',
      t5b,
    ),
    ...notes(4, t5b),
    assistant(
      'Daily seat is still up. The stale heartbeat is the browser tab, not a dead daemon.\n\nI am on this path. Freeze still holds. Hard-reload if the chrome looks wedged.',
      t4,
    ),
    ...notes(13, t4),
  ];
}

export function pixelFixtureActive(): boolean {
  return Boolean((globalThis as { __JEVONS_PIXEL_FIXTURE?: unknown }).__JEVONS_PIXEL_FIXTURE);
}

/** Visual top deltas vs virtualizer start so rows sit on golden text Y. Compensates pin shift from extra paragraph boxes on b3–b5. Product path unchanged. */
export const PNG_ROW_DY = [0, 15, -3, 11, 15, 8, 12, 5, 9, 2, 5, -1] as const;

export function pixelFixtureRowTop(start: number, index: number, density?: string): number {
  if (!pixelFixtureActive()) return start;
  if (density && density !== 'comfortable') return start;
  const dy = PNG_ROW_DY[index];
  return start + (typeof dy === 'number' ? dy : 0);
}

/** Frozen plan bars matching old-cockpit-1440x900.png (claude s/w, codex w, grok w). */
export function pixelFixturePlanUsage(): {
  windows: Array<{
    provider: string;
    name: string;
    remaining_percent: number;
    resets_at: string;
    limit_window_seconds: number;
    status: string;
    pace?: string;
  }>;
} {
  const iso = (frac: number, limitSec: number) =>
    new Date(PIXEL_FIXTURE_NOW + frac * limitSec * 1000).toISOString();
  const sess = 5 * 60 * 60;
  const week = 7 * 24 * 60 * 60;
  return {
    windows: [
      { provider: 'claude', name: 'session', remaining_percent: 100, resets_at: iso(0.88, sess), limit_window_seconds: sess, status: 'available' },
      { provider: 'claude', name: 'weekly', remaining_percent: 36, pace: 'under', resets_at: iso(0.38, week), limit_window_seconds: week, status: 'available' },
      { provider: 'codex', name: 'weekly', remaining_percent: 83, pace: 'crit', resets_at: iso(0.55, week), limit_window_seconds: week, status: 'available' },
      { provider: 'grok', name: 'weekly', remaining_percent: 86, resets_at: iso(0.70, week), limit_window_seconds: week, status: 'available' },
    ],
  };
}

export function pixelFixtureAgents(): Array<{
  name: string;
  parent?: string;
  purpose?: string;
  status?: string;
  provider?: string;
  model?: string;
  workdir?: string;
}> {
  return [
    { name: 'jevons', status: 'running', provider: 'claude', model: 'claude-sonnet-4-5' },
    { name: 'PERSONAL', parent: 'jevons', purpose: 'portfolio' },
    {
      name: 'jevons-po',
      parent: 'PERSONAL',
      status: 'running',
      provider: 'claude',
      model: 'claude-sonnet-4-5',
      workdir: '/Users/marcelo/work/github.com/marcelocantos/jevons',
    },
    { name: 'SQUZ', parent: 'jevons', purpose: 'portfolio' },
  ];
}

export const PIXEL_FIXTURE_READY = 62;

export function pixelFixtureFrontier(): Array<{
  id: string;
  name: string;
  status?: string;
  fanout?: number;
}> {
  return [
    { id: 'T47', name: 'A second user can install and run jevons using only the docs', status: 'converging', fanout: 1 },
    { id: 'T358', name: 'Owner decision packet: whether root overseer and/or product owners default to Fable-class models is ratified', status: 'identified', fanout: 1 },
    { id: 'T509', name: 'Load-bearing fleet messages travel in typed envelopes validated by the daemon; prose sniffing retired where a field exists', status: 'identified', fanout: 1 },
    { id: 'T17', name: 'Mobile app for Jevon', status: 'converging' },
    { id: 'T27', name: 'Jevons is the ecosystem aggregation hub — external tools register as providers', status: 'converging' },
    { id: 'T32', name: 'Jevons builds owner-requested capabilities itself — exemplar', status: 'identified' },
    { id: 'T67', name: 'Shift-Return in the composer continues the list you are in', status: 'identified' },
    { id: 'T112', name: 'Chat markdown embedding policy covers common specialty', status: 'identified' },
    { id: 'T119.10', name: 'Live ⋯ N steps capsules and WorkingProgress N match a harness', status: 'identified' },
    { id: 'T254', name: 'No owner workstream for which Gas Town / Beads-factory', status: 'identified' },
    { id: 'T254.2', name: 'Same-repo multi-worker fan-out uses worktrees and a single', status: 'identified' },
    { id: 'T254.3', name: 'Targets can carry ordered plan steps that agents walk and resume', status: 'identified' },
    { id: 'T254.4', name: 'Fleet ops inbox delivers structured finish/block/needs-de', status: 'identified' },
    { id: 'T254.5', name: 'Stuck/idle/open-mission recovery is a standing product path', status: 'identified' },
  ];
}
