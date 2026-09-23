// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement } from 'react';
import { CoachList } from './CoachList';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('coach list', () => {
  it('shows the judgments the dispositions API already has', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        judgments: [{
          fingerprint: 'abc',
          name: 'parent_report failures stay visible',
          severity: 'medium',
          delivered_at: '2026-09-23T11:56:07Z',
          disposition: 'pending',
          evidence: 'eventlog',
        }],
        total: 1,
        pending: 1,
      }),
    }));
    render(createElement(CoachList, { active: true }));
    expect(await screen.findByText('parent_report failures stay visible')).toBeTruthy();
    expect(screen.getByText('pending')).toBeTruthy();
    expect(screen.getByText('1 pending · 1 judged')).toBeTruthy();
    expect(screen.queryByText(/port next/)).toBeNull();
  });

  it('says when the list is empty', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ judgments: [], total: 0, pending: 0 }),
    }));
    render(createElement(CoachList, { active: true }));
    expect(await screen.findByText('No coach judgments yet.')).toBeTruthy();
  });

  it('does not fetch until the tab is open', async () => {
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    render(createElement(CoachList, { active: false }));
    await waitFor(() => expect(fetch).not.toHaveBeenCalled());
  });
});
