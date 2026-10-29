// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const src = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), 'AgentTranscript.tsx'),
  'utf8',
);

describe('T532 idle settle pin', () => {
  it('uses pin residual slack before rewriting scrollTop on canvas resize', () => {
    const effect = src.slice(src.indexOf('🎯T532'), src.indexOf('ro.observe(canvas)'));
    expect(effect).toContain('settlePinGapPx');
    expect(effect).not.toMatch(/if \(gap <= 0\) return/);
  });
});
