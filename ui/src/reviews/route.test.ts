// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { router } from '../App';

describe('review deep link', () => {
  it('keeps an opaque review version identity in the detail route, not in the search pane', () => {
    const detail = router.buildLocation({ to: '/reviews/$reviewId', params: { reviewId: 'question-v2-opaque' } });
    expect(detail.pathname).toBe('/reviews/question-v2-opaque');
    expect(router.buildLocation({ to: '/', search: { agent: '', tab: 'reviews' } }).href).toContain('tab=reviews');
  });
});
