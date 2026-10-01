// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FrontierTable } from './FrontierTable';
import type { SeatWaits } from '../frontier/play';

// 🎯T980: play is a nudge. Its row shows the PO acknowledged it; when no plan
// can seat the work it turns into a red arrow that force-seats on click, with
// stop offered to its left on hover.
describe('FrontierTable play states (🎯T980)', () => {
  afterEach(cleanup);
  const rows = [{ id: 'T979', name: 'Plan-band override indicator', status: 'identified' }];
  const agents = [{ name: 'jevons-po', running: true, purpose: 'work' }];

  function setup(waits: SeatWaits) {
    const calls: { url: string; body: string }[] = [];
    const fetcher = async (url: string, init: { body: string }) => {
      calls.push({ url, body: init.body });
      return { ok: true, status: 200 };
    };
    const view = render(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      <FrontierTable rows={rows as any} agents={agents} fetcher={fetcher as any} seatWaits={async () => waits} />,
    );
    const btn = () => view.container.querySelector<HTMLButtonElement>('td.ft-play button[data-play-mode]')!;
    return { view, calls, btn };
  }

  it('shows acknowledged once the PO has the kickoff', async () => {
    const { calls, btn } = setup({});
    expect(btn().getAttribute('data-play-mode')).toBe('play');
    await act(async () => btn().click());
    await waitFor(() => expect(btn().getAttribute('data-play-mode')).toBe('acked'));
    expect(calls[0].url).toBe('/api/agents/jevons-po/send');
    expect(calls[0].body).not.toContain('force-play');
  });

  it('turns into a red arrow that force-seats, with stop to its left on hover', async () => {
    const { view, calls, btn } = setup({ T979: { reason: 'claude: weekly ahead of pace (62% used)', at: '2026-10-01T05:00:00Z' } });
    await waitFor(() => expect(btn().getAttribute('data-play-mode')).toBe('waiting'));
    expect(btn().className).toContain('ft-waiting-btn');
    expect(btn().title).toContain('weekly ahead of pace');
    const wrap = view.container.querySelector('.ft-play-wrap')!;
    expect(view.container.querySelector('.ft-wait-stop')).toBeNull();
    fireEvent.mouseEnter(wrap);
    const stop = view.container.querySelector<HTMLButtonElement>('.ft-wait-stop');
    expect(stop).not.toBeNull();
    expect(stop!.compareDocumentPosition(btn()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    fireEvent.mouseLeave(wrap);
    expect(view.container.querySelector('.ft-wait-stop')).toBeNull();

    await act(async () => btn().click());
    const force = calls.find((c) => c.url === '/api/agents/jevons-po/send');
    expect(force?.body).toContain('Owner force-play');
    expect(force?.body).toContain('owner_asked=true');
    await waitFor(() => expect(btn().getAttribute('data-play-mode')).toBe('acked'));
  });

  it('stop on hover withdraws a waiting request', async () => {
    const { view, calls, btn } = setup({ T979: { reason: 'no plan', at: 'a' } });
    await waitFor(() => expect(btn().getAttribute('data-play-mode')).toBe('waiting'));
    fireEvent.mouseEnter(view.container.querySelector('.ft-play-wrap')!);
    await act(async () => view.container.querySelector<HTMLButtonElement>('.ft-wait-stop')!.click());
    expect(calls.some((c) => c.url === '/api/agents/engagement/stop' && c.body.includes('T979'))).toBe(true);
    expect(btn().getAttribute('data-play-mode')).toBe('play');
  });
});
