// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('aside-sidebar'), () => {
  itOracle('T309.1', 'main and sidebar mount the same AgentInteraction widget', () => {
    const root = join(dirname(fileURLToPath(import.meta.url)), '../..');
    const app = readFileSync(join(root, 'App.tsx'), 'utf8');
    const mounts = app.match(/<AgentInteraction\b/g) || [];
    expect(mounts.length).toBeGreaterThanOrEqual(2);
  });

  itOracle.todo('T65', 'attention threads live in human↔overseer chat — park/pursue, not a commit');
  itOracle.todo('T95', 'target: prefix is a short-lived aside that files and auto-closes');
  itOracle.todo('T124', 'RHS selection shows that agent’s full transcript out of band from main');
  itOracle.todo('T250', 'asides are not visible in the main transcript');
  itOracle.todo('T251', 'sidebar transcript has its own composer and send UX');
  itOracle.todo('T265', 'aside Transcript is a microcosm of main — same look, no nested sidebar');
  itOracle.todo('T367', 'sidebar messages persist across reload and daemon reboot');
  itOracle.todo('T371', 'aside and main composers share one send/display path');
  itOracle.todo('T372', 'one chat widget + one agent contract; seats differ only by role');
});
