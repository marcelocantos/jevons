// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, expect, vi } from 'vitest';
import { CoachPane } from '../../components/CoachPane';
import { SidebarPanel } from '../../components/SidebarPanel';
import {
  countsText,
  detailText,
  emptyText,
  normalizePayload,
  parseTime,
  row,
} from '../../coach/dispositions';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const PAYLOAD = {
  total: 3,
  count: 3,
  pending: 1,
  path: '/tmp/state/rsi/dispositions.json',
  judgments: [
    {
      fingerprint: 'fp-pending',
      name: 'chat gap',
      observation: 'owner asked twice before a reply landed',
      severity: 'medium',
      delivered_at: '2026-08-09T03:00:00Z',
      disposition: 'pending',
      evidence: 'owner_chat:chatlog-2026-08-09 (chat_gap)',
    },
    {
      fingerprint: 'fp-ignored',
      name: 'phrase friction',
      observation: 'repeat phrasing in overseer replies',
      severity: 'low',
      delivered_at: '2026-08-09T02:00:00Z',
      disposition: 'ignore_with_reason',
      disposition_at: '2026-08-09T03:01:00Z',
      reason: 'one-off, no standing pattern',
    },
    {
      fingerprint: 'fp-filed',
      name: 'repair churn',
      observation: 'three follow-up commits on the same file',
      severity: 'high',
      mode: 'retro',
      delivered_at: '2026-08-09T01:00:00Z',
      disposition: 'file',
      disposition_at: '2026-08-09T03:02:00Z',
      target_id: 'T999',
    },
  ],
};

describeOracle(family('coach'), () => {
  itOracle('T354', 'Coach tab lists durable judgments and dispositions from the product API', async () => {
    const empty = normalizePayload({ judgments: [], total: 0, pending: 0, count: 0 });
    expect(empty.empty).toBe(true);
    expect(empty.error).toBe('');
    expect(emptyText().length).toBeGreaterThan(0);
    expect(countsText(empty)).toBe('');

    const failed = normalizePayload(null, new Error('HTTP 500'));
    expect(failed.error).toBe('HTTP 500');
    expect(failed.empty).toBe(false);

    const m = normalizePayload(PAYLOAD);
    expect(m.total).toBe(3);
    expect(m.pending).toBe(1);
    expect(m.rows.map((r) => r.fingerprint)).toEqual(['fp-pending', 'fp-ignored', 'fp-filed']);
    expect(m.rows[0].pending).toBe(true);
    expect(detailText(m.rows[1])).toMatch(/one-off/);
    expect(m.rows[2].retro).toBe(true);
    expect(detailText(m.rows[2])).toMatch(/🎯T999/);
    expect(countsText(m)).toBe('3 judgments · 1 pending');
    expect(parseTime('0001-01-01T00:00:00Z')).toBe(0);
    expect(row({ fingerprint: 'x', observation: 'only an observation' }).title).toBe('only an observation');

    const urls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        urls.push(String(url));
        return {
          ok: true,
          json: async () => PAYLOAD,
        };
      }),
    );

    const { container } = render(createElement(CoachPane, { active: true }));
    await waitFor(() => expect(container.querySelectorAll('.coach-row').length).toBe(3));
    expect(urls).toContain('/api/rsi/dispositions');
    expect(container.querySelector('#coach-counts')?.textContent).toBe('3 judgments · 1 pending');
    expect(container.textContent).toMatch(/chat gap/);
    expect(container.textContent).toMatch(/filed/);
    expect(container.textContent).toMatch(/ignored/);
    expect(container.textContent).toMatch(/🎯T999/);
    expect(container.textContent).toMatch(/⏮/);
    expect(container.querySelector('.coach-chip.coach-pending')?.textContent).toBe('pending');
    fireEvent.click(container.querySelector('#coach-refresh')!);
    await waitFor(() => expect(urls.filter((u) => u === '/api/rsi/dispositions').length).toBeGreaterThanOrEqual(2));

    const sidebar = render(createElement(SidebarPanel, { tab: 'coach', onTab: () => {}, children: createElement('div') }));
    expect(sidebar.container.querySelector('#coach-pane')?.getAttribute('aria-label')).toBe('RSI coach judgments');
    expect(sidebar.container.textContent).not.toMatch(/Coach judgments port next/);
  });
});
