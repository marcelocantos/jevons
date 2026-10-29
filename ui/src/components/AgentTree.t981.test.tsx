// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentTree } from './AgentTree';

describe('AgentTree seat pause (🎯T981)', () => {
  afterEach(cleanup);

  it('shows ❚❚ on a running seat and calls pause', () => {
    const onPause = vi.fn();
    const view = render(
      <AgentTree
        agents={[{ name: 'jv-t981-auto', running: true, purpose: 'work' }]}
        selected=""
        onSelect={() => {}}
        onPause={onPause}
      />,
    );
    const btn = view.container.querySelector<HTMLButtonElement>('.agent-pause-btn')!;
    expect(btn.textContent).toBe('❚❚');
    fireEvent.click(btn);
    expect(onPause).toHaveBeenCalledWith('jv-t981-auto', true);
  });

  it('shows ▶ on a paused seat and calls unpause', () => {
    const onPause = vi.fn();
    const view = render(
      <AgentTree
        agents={[{ name: 'jv-t981-auto', running: true, purpose: 'work', owner_paused: true }]}
        selected=""
        onSelect={() => {}}
        onPause={onPause}
      />,
    );
    const btn = view.container.querySelector<HTMLButtonElement>('.agent-pause-btn-active')!;
    expect(btn.textContent).toBe('▶');
    fireEvent.click(btn);
    expect(onPause).toHaveBeenCalledWith('jv-t981-auto', false);
  });
});
