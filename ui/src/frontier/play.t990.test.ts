// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  applySeatWaits,
  buildPlayKickoffText,
  ledgerOwningPO,
  playChromeSpec,
  playKickoffRequest,
  resolvePlayPO,
  type PlayAgent,
  type PlayRow,
} from './play';

// 🎯T990: on 2026-10-02 the owner force-played 🎯T989 — a target filed in
// jevons's own ledger whose code lives in jevons-mobile — while yourworld2-po
// was the selected seat. The kickoff went to yourworld2-po, which minted the
// worker under itself. The ledger the table shows owns its targets; its PO
// is the recipient, whatever is selected.
const JEVONS = '/Users/o/work/github.com/marcelocantos/jevons/bullseye.yaml';
const YOURWORLD = '/Users/o/work/github.com/squz/yourworld2/bullseye.yaml';
const agents: PlayAgent[] = [
  { name: 'jevons', purpose: 'overseer' },
  { name: 'jevons-po', purpose: 'work', role: 'product-owner', parent: 'jevons', ledger: JEVONS, running: true },
  { name: 'yourworld2-po', purpose: 'work', role: 'product-owner', parent: 'jevons', ledger: YOURWORLD, running: true },
  { name: 'yw-t12-worker', purpose: 'work', role: 'worker', parent: 'yourworld2-po', ledger: YOURWORLD, running: true },
];
const t989: PlayRow = {
  id: 'T989',
  name: 'jevons-mobile Flutter shell wraps the cockpit',
  status: 'identified',
  acceptance: ['The app opens the cockpit in a WebView over the pigeon relay'],
};

describe('force-play routes to the ledger-owning PO (🎯T990)', () => {
  it('names the PO whose workdir resolves to the ledger', () => {
    expect(ledgerOwningPO(agents, JEVONS)).toBe('jevons-po');
    expect(ledgerOwningPO(agents, YOURWORLD)).toBe('yourworld2-po');
    expect(ledgerOwningPO(agents, '')).toBe('');
    expect(ledgerOwningPO(agents, '/elsewhere/bullseye.yaml')).toBe('');
  });

  it('prefers a running PO for the ledger, deterministically', () => {
    const two: PlayAgent[] = [
      { name: 'b-po', purpose: 'work', ledger: JEVONS, running: true },
      { name: 'a-po', purpose: 'work', ledger: JEVONS, running: false },
    ];
    expect(ledgerOwningPO(two, JEVONS)).toBe('b-po');
    expect(ledgerOwningPO(two.map((a) => ({ ...a, running: true })), JEVONS)).toBe('a-po');
  });

  it('the selected seat does not pick the recipient once the table is bound to a ledger', () => {
    for (const selected of ['yourworld2-po', 'yw-t12-worker', 'jevons', '']) {
      expect(resolvePlayPO({ agents, selectedAgent: selected, ledgerKey: JEVONS })).toBe('jevons-po');
    }
    const req = playKickoffRequest(t989, { agents, selectedAgent: 'yourworld2-po', ledgerKey: JEVONS, force: true });
    expect(req.blocked).toBe(false);
    expect(req.po).toBe('jevons-po');
    if (!req.blocked) {
      expect(req.url).toBe('/api/agents/jevons-po/send');
      expect(req.body.text).toContain('parent=jevons-po');
      expect(req.body.text).not.toContain('yourworld2-po');
    }
  });

  it('a selected PO on a different ledger is never the fallback; the default is', () => {
    const unowned = '/Users/o/work/github.com/marcelocantos/orphan/bullseye.yaml';
    expect(resolvePlayPO({ agents, selectedAgent: 'yourworld2-po', ledgerKey: unowned })).toBe('jevons-po');
    expect(resolvePlayPO({ agents, selectedAgent: 'yw-t12-worker', ledgerKey: unowned })).toBe('jevons-po');
  });

  it('without a ledger the 🎯T255 selected-agent rule still applies', () => {
    expect(resolvePlayPO({ agents, selectedAgent: 'yw-t12-worker' })).toBe('yourworld2-po');
    expect(resolvePlayPO({ agents, selectedAgent: 'jevons' })).toBe('jevons-po');
  });
});

// 🎯T990: a seat that was minted and died before its brief landed is a
// different thing from a plan that cannot take a seat. The row says which,
// and the force-play brief tells the PO what killed the last seat.
describe('a failed seat is shown, not bounced back to play (🎯T990)', () => {
  const reason =
    'seat jv-t989-mobile-webview (parent yourworld2-po) retired: unbriefed_seat — retired a seat whose opening brief never landed (🎯T433); ' +
    'Claude Code has not trusted workdir /Users/o/work/github.com/marcelocantos/jevons-mobile';

  it('overlays the kind and seat from GET /api/seat-waits', () => {
    const [row] = applySeatWaits([{ ...t989, engaged: false }], {
      T989: { reason, at: '2026-10-02T09:14:25Z', kind: 'seat_failed', seat: 'jv-t989-mobile-webview', parent: 'yourworld2-po' },
    });
    expect(row.seat_wait_kind).toBe('seat_failed');
    expect(row.seat_wait_seat).toBe('jv-t989-mobile-webview');
    expect(row.seat_wait).toBe(reason);
    const [plan] = applySeatWaits([{ ...t989, engaged: false }], { T989: { reason: 'claude: weekly hot', at: 'x' } });
    expect(plan.seat_wait_kind).toBe('plan');
  });

  it('paints the red arrow with the failure in its title', () => {
    const [row] = applySeatWaits([{ ...t989, engaged: false }], { T989: { reason, at: 'x', kind: 'seat_failed' } });
    const spec = playChromeSpec(row, { agents, ledgerKey: JEVONS });
    expect(spec.mode).toBe('waiting');
    expect(spec.className).toContain('ft-seat-failed-btn');
    expect(spec.title).toContain('Seat failed to start');
    expect(spec.title).toContain('unbriefed_seat');
    expect(spec.title).toContain('has not trusted workdir');
    expect(spec.ariaLabel).toContain('failed to start');
    expect(spec.disabled).toBe(false);
  });

  it('the force-play brief carries the previous attempt to the PO', () => {
    const [row] = applySeatWaits([{ ...t989, engaged: false }], { T989: { reason, at: 'x', kind: 'seat_failed' } });
    const text = buildPlayKickoffText(row, { agents, ledgerKey: JEVONS, force: true });
    expect(text).toContain('Owner force-play');
    expect(text).toContain('Previous attempt: ' + reason);
    const plain = buildPlayKickoffText(t989, { agents, ledgerKey: JEVONS, force: true });
    expect(plain).not.toContain('Previous attempt');
  });
});
