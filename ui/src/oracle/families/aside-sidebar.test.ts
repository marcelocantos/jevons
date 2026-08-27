// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { AgentTree } from '../../components/AgentTree';
import { isAsidePurpose } from '../../fleet/rowModel';
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
  itOracle('T269', 'aside rows carry a hover-gated × that dismisses without selecting', () => {
    expect(isAsidePurpose('aside')).toBe(true);
    expect(isAsidePurpose('File-Target')).toBe(true);
    expect(isAsidePurpose('work')).toBe(false);
    expect(isAsidePurpose(undefined)).toBe(false);
    const selected: string[] = [];
    const dismissed: string[] = [];
    const { container } = render(
      createElement(AgentTree, {
        agents: [
          { name: 'jevons', purpose: 'overseer' },
          { name: 'jevons-po', purpose: 'po', parent: 'jevons' },
          { name: 'jv-t1-work', purpose: 'work', parent: 'jevons-po' },
          { name: 'aside-1', purpose: 'aside', parent: 'jevons' },
        ],
        selected: '',
        onSelect: (n: string) => selected.push(n),
        onDismiss: (n: string) => dismissed.push(n),
      }),
    );
    const xs = [...container.querySelectorAll<HTMLButtonElement>('.agent-dismiss')];
    expect(xs.map((b) => b.dataset.agentDismiss)).toEqual(['aside-1']);
    expect(container.querySelectorAll('.agent-node.agent-aside').length).toBe(1);
    // Hover-only is CSS: hidden until the aside row is hovered/focused.
    const css = readFileSync(join(root, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/\.agent-node \.agent-dismiss \{[^}]*opacity: 0/);
    expect(css).toMatch(/\.agent-node\.agent-aside:hover \.agent-dismiss/);
    fireEvent.click(xs[0]);
    expect(dismissed).toEqual(['aside-1']);
    expect(selected).toEqual([]);
  });
});
