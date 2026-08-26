// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const suiteDir = join(dirname(fileURLToPath(import.meta.url)), '../../../../scripts/journey-suite');
const src = ['chrome_journeys.go', 'react_paint.js']
  .map((n) => readFileSync(join(suiteDir, n), 'utf8'))
  .join('\n');

describeOracle(family('journey-fold-md'), () => {
  itOracle(
    ['T59', 'T106', 'T238'],
    'J23 asserts mermaid svg, .msg-clipped pocket, and no [silent] owner bubble',
    () => {
      expect(src).toMatch(/jFoldMd/);
      expect(src).toMatch(/msg-clipped/);
      expect(src).toMatch(/msg-expand-tab/);
      expect(src).toMatch(/mermaid/);
      expect(src).toMatch(/\[silent\]/);
      expect(src).toMatch(/14rem/);
    },
  );
});
