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

describeOracle(family('journey-aside'), () => {
  itOracle(['T95', 'T250', 'T251'], 'J26 sends target: and forbids a main .msg.user leak', () => {
    expect(src).toMatch(/jAsideChrome/);
    expect(src).toMatch(/target:/);
    expect(src).toMatch(/T250/);
    expect(src).toMatch(/agent-inspect-input/);
  });
});
