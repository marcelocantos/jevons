// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { companyOfProvider, windowAbbrev } from './companyMark';

describe('plan company marks', () => {
  it('maps providers to the same companies the old header uses', () => {
    expect(companyOfProvider('claude')).toBe('anthropic');
    expect(companyOfProvider('grok')).toBe('xai');
    expect(companyOfProvider('codex')).toBe('openai');
  });

  it('abbreviates windows as s / w', () => {
    expect(windowAbbrev('session')).toBe('s');
    expect(windowAbbrev('weekly')).toBe('w');
  });
});
