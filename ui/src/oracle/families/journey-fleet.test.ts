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

describeOracle(family('journey-fleet'), () => {
  itOracle(['T68', 'T72', 'T72.1'], 'J25 mints a live agent and requires it in the vanilla #agents tree', () => {
    expect(src).toMatch(/jFleetSidebar/);
    expect(src).toMatch(/jv-t540-fleet-oracle/);
    expect(src).toMatch(/#agents/);
    expect(src).toMatch(/agent-node/);
    expect(src).toMatch(/T72\.1/);
  });
});
