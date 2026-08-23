// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { clipClassName, expandTabChevron, shouldClip } from './clip';

describe('shouldClip', () => {
  it('clips a tall user wall and leaves a short bubble', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(100)).toBe(false);
    expect(shouldClip(224)).toBe(false);
    expect(shouldClip(226)).toBe(true);
  });
});

describe('clipClassName', () => {
  it('adds msg-clipped only when tall', () => {
    expect(clipClassName('bubble bubble-user', 400)).toContain('msg-clipped');
    expect(clipClassName('bubble bubble-user', 80)).toBe('bubble bubble-user');
  });
});

describe('expandTabChevron', () => {
  it('matches the vanilla pocket-tab glyphs', () => {
    expect(expandTabChevron(false)).toBe('\u25BE');
    expect(expandTabChevron(true)).toBe('\u25B4');
  });
});
