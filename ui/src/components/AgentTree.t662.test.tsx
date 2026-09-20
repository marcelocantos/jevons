// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AgentTree, massStopLine } from './AgentTree';

// 🎯T662: the RHS fleet surface shows one mass-stop alert and, on each
// stopped row, the reason the daemon recorded — never a bare stopped dot.
describe('AgentTree seat stops (🎯T662)', () => {
  const mass =
    'MASS STOP (🎯T662): 3 seats stopped within 1m0s (20:52:13–20:53:05) with no daemon restart in the window — unknown: 3 seats, no reason recorded. Seats: jv-t657-steer-ui, jv-t658-hop-classifier, jv-t659-clean-web-gate.';

  it('paints the mass-stop alert once and the recorded reason on stopped rows', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'jevons-po', running: true, mass_stop: mass },
          {
            name: 'jv-t657-steer-ui',
            parent: 'jevons-po',
            running: false,
            stop_reason: 'unknown: process exited and no reason was recorded',
            mass_stop: mass,
          },
          {
            name: 'jv-t658-hop-classifier',
            parent: 'jevons-po',
            running: false,
            stop_reason: 'jevons_agent_stop by jevons: parked for the night',
            mass_stop: mass,
          },
        ]}
      />,
    );
    const alerts = container.querySelectorAll('.fleet-mass-stop');
    expect(alerts.length).toBe(1);
    expect(alerts[0].textContent).toContain('MASS STOP');
    expect(alerts[0].textContent).toContain('unknown: 3 seats, no reason recorded');

    const reasons = [...container.querySelectorAll('.agent-stop-reason')].map((n) => n.textContent);
    expect(reasons).toEqual([
      '⛔ unknown: process exited and no reason was recorded',
      '⛔ jevons_agent_stop by jevons: parked for the night',
    ]);
    // The running PO carries the line for the alert but no reason of its own.
    const po = [...container.querySelectorAll('.agent-node')].find((n) =>
      n.querySelector('.agent-name')?.textContent === 'jevons-po',
    );
    expect(po?.querySelector('.agent-stop-reason')).toBeNull();
  });

  it('shows nothing extra when the fleet is healthy', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[{ name: 'jevons-po', running: true }, { name: 'jv-x', parent: 'jevons-po', running: true }]}
      />,
    );
    expect(container.querySelector('.fleet-mass-stop')).toBeNull();
    expect(container.querySelector('.agent-stop-reason')).toBeNull();
    expect(massStopLine([{ name: 'a' }, { name: 'b', mass_stop: ' ' }])).toBe('');
  });
});
