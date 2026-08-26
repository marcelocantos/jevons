// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const suiteDir = join(dirname(fileURLToPath(import.meta.url)), '../../../../scripts/journey-suite');
const src = ['chrome_journeys.go', 'react_paint.js', 'react_surface.go']
  .map((n) => readFileSync(join(suiteDir, n), 'utf8'))
  .join('\n');

describeOracle(family('journey-send'), () => {
  itOracle(
    ['T279', 'T281', 'T504'],
    'J22 asserts one .msg.user per send and the reply sits below it — vanilla chrome, not today’s React tree',
    () => {
      expect(src).toMatch(/jSendOnce/);
      expect(src).toMatch(/scenarioSend|#send/);
      expect(src).toMatch(/\.msg\.user/);
      expect(src).toMatch(/T281/);
      expect(src).toMatch(/T504/);
      expect(src).toMatch(/RefuseDaily/);
      expect(src).not.toMatch(/write the test so React goes green/);
    },
  );
});
