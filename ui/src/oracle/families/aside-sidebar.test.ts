// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const root = join(dirname(fileURLToPath(import.meta.url)), '../..');
const app = readFileSync(join(root, 'App.tsx'), 'utf8');
const userReq = readFileSync(join(root, 'components/UserRequest.tsx'), 'utf8');

describeOracle(family('aside-sidebar'), () => {
  itOracle('T309.1', 'main and sidebar mount the same AgentInteraction widget', () => {
    const mounts = app.match(/<AgentInteraction\b/g) || [];
    expect(mounts.length).toBeGreaterThanOrEqual(2);
  });

  itOracle('T371', 'aside and main composers share one send/display path', () => {
    expect(userReq).toMatch(/data-composer=\{props\.name === 'jevons' \? 'main' : 'sidebar'\}/);
    expect((app.match(/<AgentInteraction\b/g) || []).length).toBeGreaterThanOrEqual(2);
  });

  itOracle('T372', 'one chat widget + one agent contract; seats differ only by role', () => {
    expect(app).toContain("name=\"jevons\"");
    expect(app).toMatch(/AgentInteraction/);
    expect(app).not.toMatch(/class SidebarComposer|function SidebarComposer/);
  });

  itOracle.skip('T65', 'attention threads live in human↔overseer chat — park/pursue, not a commit', 'journey is the arbiter (J26)');
  itOracle.skip('T95', 'target: prefix is a short-lived aside that files and auto-closes', 'journey is the arbiter (J26)');
  itOracle.skip('T124', 'RHS selection shows that agent’s full transcript out of band from main', 'journey is the arbiter (J25/J26)');
  itOracle.skip('T250', 'asides are not visible in the main transcript', 'journey is the arbiter (J26)');
  itOracle.skip('T251', 'sidebar transcript has its own composer and send UX', 'journey is the arbiter (J26)');
  itOracle.skip('T265', 'aside Transcript is a microcosm of main — same look, no nested sidebar', 'journey is the arbiter (J26)');
  itOracle.skip('T367', 'sidebar messages persist across reload and daemon reboot', 'journey is the arbiter (J26 persist residual)');
  itOracle.skip('T269', 'aside hover-× dismisses the chip', 'not ported: InstantTip-class hover dismiss');
});
