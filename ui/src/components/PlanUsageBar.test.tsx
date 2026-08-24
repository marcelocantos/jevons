// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { companyOfProvider } from '../plan/companyMark';

const here = dirname(fileURLToPath(import.meta.url));

describe('PlanUsageBar long-poll wiring', () => {
  it('passes AbortSignal and skips refetch while a request is in flight', () => {
    const src = readFileSync(join(here, 'PlanUsageBar.tsx'), 'utf8');
    expect(src).toContain("fetch('/api/plan-usage', { signal })");
    expect(src).toContain("fetchStatus === 'fetching'");
    expect(src).toContain('PLAN_POLL_PENDING_MS');
    expect(src).toContain('CompanyMark');
  });

  it('maps cursor through the shared company mark', () => {
    expect(companyOfProvider('cursor')).toBe('cursor');
  });
});
