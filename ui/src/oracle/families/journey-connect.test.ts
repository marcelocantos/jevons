// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

/**
 * Journey family. Hermetic here only ratchets that J19 still exists and
 * names the React retarget. The live oracle is `make test-journey` J19.
 */
describeOracle(family('journey-connect'), () => {
  itOracle(['T491', 'T493', 'T494'], 'J19 remains the connect-tail journey (retarget at React)', () => {
    const src = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../../../scripts/journey-suite/j19_root_history.go'),
      'utf8',
    );
    expect(src).toMatch(/j19RootHistoryPaint/);
    expect(src).toMatch(/T494/);
    expect(src).toMatch(/RefuseDaily/);
  });

  itOracle.todo('T494', 'J19 hard-load paints the React replay tail (isolate GET / or :5173)');
});
