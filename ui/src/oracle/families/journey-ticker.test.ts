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

describeOracle(family('journey-ticker'), () => {
  itOracle(['T390', 'T248'], 'J28 requires a visible #plan-ticker and an RHS resize handle', () => {
    expect(src).toMatch(/jTickerChrome/);
    expect(src).toMatch(/plan-ticker/);
    expect(src).toMatch(/rhs-width-handle/);
    expect(src).toMatch(/empty stub/);
  });
});
