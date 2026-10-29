// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { reviewAnswerDraft, type ReviewItem } from './ReviewPanel';

function item(partial: Partial<ReviewItem>): ReviewItem {
  return {
    schema_version: 2,
    target_lookup: 'verified',
    evidence: {
      commit: { status: 'missing' },
      gate: { status: 'missing' },
      diff: { status: 'missing' },
      report: { status: 'missing' },
      screenshots: [],
    },
    id: 'abc',
    url: '/api/reviews/abc',
    repository: 'jevons',
    target: 'T44.6',
    ask_id: 'ask1',
    version: 'v1',
    question: 'Which cube-map pick?',
    asker: 'jevons-po',
    answer_route: 'jevons-po',
    default_on_silence: 'defer',
    state: 'open',
    readiness: 'actionable',
    ...partial,
  };
}

describe('reviewAnswerDraft', () => {
  it('names the target, repo and question', () => {
    const d = reviewAnswerDraft(item({}));
    expect(d).toContain('🎯T44.6');
    expect(d).toContain('jevons');
    expect(d).toContain('Which cube-map pick?');
  });

  it('includes options when present', () => {
    const d = reviewAnswerDraft(item({ options: ['a', 'b'] }));
    expect(d).toContain('Options: a | b');
  });

  it('includes target title when known', () => {
    const d = reviewAnswerDraft(item({ target_title: 'Cube-map pick' }));
    expect(d).toContain('Cube-map pick');
  });

  it('never silently drops the question', () => {
    const d = reviewAnswerDraft(item({ question: '' }));
    expect(d).toContain('(no question text)');
  });
});

import { reviewIsStalling } from './ReviewPanel';

describe('reviewIsStalling', () => {
  it('flags the STALLS contract prefix', () => {
    expect(reviewIsStalling('STALLS — asker stated no default')).toBe(true);
    expect(reviewIsStalling('proceeds on default: cockpit')).toBe(false);
  });
});
