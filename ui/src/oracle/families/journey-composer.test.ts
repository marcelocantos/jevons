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

describeOracle(family('journey-composer'), () => {
  itOracle(['T123', 'T126', 'T478'], 'J24 asserts Home/End and empty composer one-control-tall vs #send', () => {
    expect(src).toMatch(/jComposerChrome/);
    expect(src).toMatch(/T126/);
    expect(src).toMatch(/T123/);
    expect(src).toMatch(/#input/);
    expect(src).toMatch(/selectionStart/);
  });
});
