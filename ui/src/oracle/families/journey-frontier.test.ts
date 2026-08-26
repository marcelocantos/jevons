// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const suiteDir = join(dirname(fileURLToPath(import.meta.url)), '../../../../scripts/journey-suite');
const src = ['chrome_journeys.go', 'react_paint.js'].map((n) => readFileSync(join(suiteDir, n), 'utf8')).join('\n');

describeOracle(family('journey-frontier'), () => {
  itOracle(['T131', 'T173', 'T185'], 'J27 requires Frontier tab, headerless table, and Graph opening a large panel', () => {
    expect(src).toMatch(/jFrontierChrome/);
    expect(src).toMatch(/rhs-tab-frontier/);
    expect(src).toMatch(/headerless/);
    expect(src).toMatch(/T185/);
    expect(src).toMatch(/frontier-graph/);
  });
});
