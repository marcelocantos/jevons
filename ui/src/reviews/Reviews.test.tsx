// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ReviewDetail, ReviewsList, reviewDraft, reviewReadiness, verifiedEvidenceLinks, type Review } from './Reviews';
import { useDrafts } from '../store/drafts';

const review: Review = {
  id: 'opaque-v2', url: '/api/reviews/opaque-v2', repository: 'jevons', target: 'T1044', target_lookup: 'verified',
  target_title: 'Owner review in chat', ask_id: 'owner-question', version: 'v2', question: 'Is this acceptable?',
  state: 'open', readiness: 'actionable', evidence: {
    commit: { status: 'verified', reference: 'a'.repeat(40) },
    gate: { status: 'verified', url: '/api/reviews/opaque-v2/gate', reference: 'abc12345', verdict: 'GREEN' },
    diff: { status: 'verified', url: '/api/reviews/opaque-v2/diff' },
    screenshots: [{ status: 'reported_only', reference: 'capture.png' }, { status: 'verified', url: 'javascript:alert(1)' }],
    report: { status: 'verified', url: '/api/reviews/opaque-v2/report', reference: 'agent/report-handle' },
  },
};

function wrap(node: React.ReactNode) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>);
}

beforeEach(() => { useDrafts.setState({ drafts: {} }); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe('versioned review and ordinary chat handoff', () => {
  it('discovers reviews and opens the opaque versioned identity', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => [review] }));
    const open = vi.fn();
    wrap(<ReviewsList onOpen={open} />);
    fireEvent.click(await screen.findByRole('button', { name: /jevons · T1044/ }));
    expect(open).toHaveBeenCalledWith('opaque-v2');
  });

  it('fetches exact version and only pre-fills editable root draft; never submits or resolves', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, json: async () => review });
    vi.stubGlobal('fetch', fetcher);
    const onAnswer = vi.fn();
    wrap(<ReviewDetail id="opaque-v2" onBack={() => {}} onAnswer={onAnswer} />);
    expect(await screen.findByText('Is this acceptable?')).toBeTruthy();
    expect(screen.getByText(/screenshots 1: reported only/)).toBeTruthy();
    expect(screen.getByRole('link', { name: /gate/ }).getAttribute('href')).toBe('/api/reviews/opaque-v2/gate');
    expect(screen.queryByRole('link', { name: /screenshots/ })).toBeNull();
    expect(screen.getByRole('link', { name: /diff/ }).getAttribute('href')).toBe('/api/reviews/opaque-v2/diff');
    fireEvent.click(screen.getByRole('button', { name: 'Answer in chat' }));
    expect(onAnswer).toHaveBeenCalledOnce();
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledWith('/api/reviews/opaque-v2');
    expect(useDrafts.getState().drafts.jevons).toContain('Review opaque-v2');
    expect(useDrafts.getState().drafts.jevons).toContain('Question version: v2');
    expect(useDrafts.getState().drafts.jevons).toContain('Review link: /reviews/opaque-v2');
    expect(useDrafts.getState().drafts.jevons).toContain('My answer: ');
    expect(useDrafts.getState().drafts.jevons).not.toContain('/not-a-link');
  });

  it('does not infer a prerequisite or invent links for a legacy item', () => {
    const legacy = { ...review, readiness: 'unspecified', prerequisite: '', evidence: undefined, target_lookup: 'missing' };
    expect(reviewReadiness(legacy)).toMatch(/event missing/);
    expect(verifiedEvidenceLinks(legacy)).toEqual([]);
    expect(reviewDraft(legacy)).not.toContain('Owner review in chat');
  });

  it('shows an explicit failed lookup rather than implying a question was loaded', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 404 }));
    wrap(<ReviewDetail id="old-version" onBack={() => {}} onAnswer={() => {}} />);
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Review not found'));
    expect(screen.queryByRole('button', { name: 'Answer in chat' })).toBeNull();
  });
});
