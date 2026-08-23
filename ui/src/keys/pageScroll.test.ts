// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { pageScrollDelta, TRANSCRIPT_SCROLL_SEL } from './pageScroll';

describe('pageScrollDelta', () => {
  it('PageUp goes up 0.8 viewport, PageDown down', () => {
    expect(pageScrollDelta('PageUp', 1000)).toBe(-800);
    expect(pageScrollDelta('PageDown', 1000)).toBe(800);
    expect(pageScrollDelta('Home', 1000)).toBe(0);
  });

  it('transcript pane selector is #messages, not a missing .agent-transcript (🎯T537.2.8)', () => {
    expect(TRANSCRIPT_SCROLL_SEL).toContain('#messages');
    expect(TRANSCRIPT_SCROLL_SEL).not.toContain('agent-transcript');
  });
});
