// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { AgentTree } from './AgentTree';

// 🎯T970: on 2026-09-30 four new workers read "stopped" with "unknown:
// process exited and no reason was recorded" for up to two minutes while
// they waited to launch. A seat being brought up has not stopped.
describe('AgentTree starting seats (🎯T970)', () => {
  afterEach(cleanup);

  it('paints a starting seat as starting, with no stop line', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'jevons-po', running: true },
          { name: 'jv-t947-plan-token', parent: 'jevons-po', running: false, starting: true },
        ]}
      />,
    );
    const row = [...container.querySelectorAll('.agent-node')].find((n) =>
      n.textContent?.includes('jv-t947-plan-token'),
    );
    expect(row).toBeTruthy();
    expect(row!.textContent).toContain('starting…');
    expect(row!.textContent).not.toContain('stopped');
    expect(row!.querySelector('.agent-stop-icon')).toBeNull();
  });

  it('never shows a stop reason on a starting seat, and still shows one on a stopped seat', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'jevons-po', running: true },
          {
            name: 'jv-starting',
            parent: 'jevons-po',
            running: false,
            starting: true,
            stop_reason: 'unknown: process exited and no reason was recorded',
          },
          { name: 'jv-failed', parent: 'jevons-po', running: false, stop_reason: 'start failed: launch timed out' },
        ]}
      />,
    );
    const reasons = [...container.querySelectorAll('.agent-stop-icon')].map((n) => n.textContent);
    expect(reasons).toEqual(['⛔']);
  });
});
