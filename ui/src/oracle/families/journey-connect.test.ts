// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const suiteDir = join(
  dirname(fileURLToPath(import.meta.url)),
  '../../../../scripts/journey-suite',
);

function readSuite(...names: string[]): string {
  return names.map((n) => readFileSync(join(suiteDir, n), 'utf8')).join('\n');
}

/**
 * Journey family. Hermetic here only ratchets that J19 still exists,
 * refuses :13705, and hard-loads the React cockpit (ui / :5173-style
 * proxy). The live oracle is `make test-journey` J19.
 */
describeOracle(family('journey-connect'), () => {
  itOracle(['T491', 'T493', 'T494'], 'J19 remains the connect-tail journey (retarget at React)', () => {
    const src = readSuite(
      'j19_root_history.go',
      'j19_react.go',
      'j19_paint.js',
      'j19_vite.config.mjs',
    );
    expect(src).toMatch(/j19RootHistoryPaint/);
    expect(src).toMatch(/T494/);
    expect(src).toMatch(/RefuseDaily/);
    expect(src).toMatch(/5173/);
    expect(src).toMatch(/T540\.2/);
    expect(src).toMatch(/#root|id="root"/);
    expect(src).toMatch(/vite-proxy|J19_ISOLATE/);
    expect(src).not.toMatch(/__transcriptRows/);
  });
});
