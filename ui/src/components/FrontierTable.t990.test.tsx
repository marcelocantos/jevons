// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FrontierTable } from './FrontierTable';
import type { PlayAgent, SeatWaits } from '../frontier/play';

// 🎯T990: the cockpit force-play of 🎯T989 on 2026-10-02 — a jevons-ledger
// target force-played while yourworld2-po was selected — went to the wrong
// PO, and when the seat died unbriefed the row bounced back to the green
// arrow. Bound to its ledger, the table sends to that ledger's PO; a failed
// seat stays on the row with its reason until a seat lands.
describe('FrontierTable force-start (🎯T990)', () => {
  afterEach(cleanup);
  const JEVONS = '/Users/o/work/github.com/marcelocantos/jevons/bullseye.yaml';
  const YOURWORLD = '/Users/o/work/github.com/squz/yourworld2/bullseye.yaml';
  const rows = [{ id: 'T989', name: 'jevons-mobile Flutter shell', status: 'identified' }];
  const agents: PlayAgent[] = [
    { name: 'jevons', purpose: 'overseer' },
    { name: 'jevons-po', purpose: 'work', parent: 'jevons', ledger: JEVONS, running: true },
    { name: 'yourworld2-po', purpose: 'work', parent: 'jevons', ledger: YOURWORLD, running: true },
  ];
  const failed: SeatWaits = {
    T989: {
      reason: 'seat jv-t989-mobile-webview (parent yourworld2-po) retired: unbriefed_seat — retired a seat whose opening brief never landed (🎯T433)',
      at: '2026-10-02T09:14:25Z',
      kind: 'seat_failed',
      seat: 'jv-t989-mobile-webview',
      parent: 'yourworld2-po',
    },
  };

  function setup(waits: SeatWaits, selectedAgent: string) {
    const calls: { url: string; body: string }[] = [];
    const fetcher = async (url: string, init: { body: string }) => {
      calls.push({ url, body: init.body });
      return { ok: true, status: 200 };
    };
    const view = render(
      <FrontierTable
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        rows={rows as any}
        agents={agents}
        selectedAgent={selectedAgent}
        frontierLedger={JEVONS}
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        fetcher={fetcher as any}
        seatWaits={async () => waits}
      />,
    );
    const btn = () => view.container.querySelector<HTMLButtonElement>('td.ft-play button[data-play-mode]')!;
    return { view, calls, btn };
  }

  it('routes play and force-play to the ledger-owning PO, not the selected one', async () => {
    const { calls, btn } = setup({}, 'yourworld2-po');
    expect(btn().title).toBe('Start work via jevons-po');
    await act(async () => btn().click());
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0].url).toBe('/api/agents/jevons-po/send');
    expect(calls[0].body).toContain('parent=jevons-po');
  });

  it('a retired seat shows as a failure with its reason, and force-play retries via the owning PO', async () => {
    const { calls, btn } = setup(failed, 'yourworld2-po');
    await waitFor(() => expect(btn().getAttribute('data-play-mode')).toBe('waiting'));
    expect(btn().getAttribute('data-seat-wait-kind')).toBe('seat_failed');
    expect(btn().className).toContain('ft-seat-failed-btn');
    expect(btn().title).toContain('Seat failed to start');
    expect(btn().title).toContain('unbriefed_seat');
    expect(btn().title).toContain('jv-t989-mobile-webview');

    await act(async () => btn().click());
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0].url).toBe('/api/agents/jevons-po/send');
    expect(calls[0].body).toContain('Owner force-play');
    expect(calls[0].body).toContain('Previous attempt: seat jv-t989-mobile-webview');
    await waitFor(() => expect(btn().getAttribute('data-play-mode')).toBe('acked'));
  });
});
