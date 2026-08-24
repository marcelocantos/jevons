// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AgentTree } from './AgentTree';

describe('AgentTree phase chrome (🎯T545.4)', () => {
  it('same-workdir worker paints idle / working · tool / blocked', () => {
    const wd = '/Users/x/work/github.com/marcelocantos/jevons';
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'jevons-po', workdir: wd, status: 'running', running: true, phase: 'idle' },
          { name: 'jv-idle', parent: 'jevons-po', workdir: wd, status: 'running', running: true, phase: 'idle' },
          { name: 'jv-work', parent: 'jevons-po', workdir: wd, status: 'running', running: true, phase: 'working', step: 'Bash' },
          { name: 'jv-block', parent: 'jevons-po', workdir: wd, status: 'running', running: true, phase: 'blocked' },
        ]}
      />,
    );
    const textOf = (name: string) => {
      const row = [...container.querySelectorAll('.agent-node')].find(
        (el) => el.querySelector('.agent-name')?.textContent === name,
      );
      return row?.querySelector('.agent-dir')?.textContent || '';
    };
    expect(textOf('jv-idle')).toBe('idle');
    expect(textOf('jv-work')).toBe('working · Bash');
    expect(textOf('jv-block')).toBe('blocked');
  });

  it('stopped overseer paints stopped, not a silent home row', () => {
    const { container } = render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          {
            name: 'jevons',
            purpose: 'overseer',
            workdir: '/Users/x/.jevons/jevons',
            status: 'stopped',
            running: false,
            phase: 'idle',
            progress: 'stopped',
          },
        ]}
      />,
    );
    const row = container.querySelector('.agent-node');
    expect(row?.querySelector('.agent-dot')?.className).toContain('stopped');
    expect(row?.querySelector('.agent-dir')?.textContent).toBe('stopped');
  });
});
