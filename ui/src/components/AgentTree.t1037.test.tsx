// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { FrontierRowsContext } from '../frontier/rows';
import { clearFrontierTargetCache, type FrontierRow } from '../frontier/table';
import { AgentTree } from './AgentTree';

const t658: FrontierRow = {
  id: 'T65.8',
  name: 'Cancel implement',
  status: 'converging',
  acceptance: ['Hotlink paints the dotted id'],
};

function tree(agents: { name: string; parent?: string }[], rows: FrontierRow[] = [t658], onSelect: (n: string) => void = () => {}) {
  return render(
    <FrontierRowsContext.Provider value={rows}>
      <AgentTree agents={agents} selected="" onSelect={onSelect} />
    </FrontierRowsContext.Provider>,
  );
}

describe('AgentTree seat-name hotlink (🎯T1037)', () => {
  afterEach(() => {
    clearFrontierTargetCache();
  });

  it('renders a dotted id as a hotspot and preserves the rest of the name', () => {
    const { container } = tree([{ name: 'ge-t65.8-cancel-implement' }]);
    const name = container.querySelector('.agent-name');
    const spot = container.querySelector('.target-hotspot');
    expect(name?.textContent).toBe('ge-t65.8-cancel-implement');
    expect(spot?.textContent).toBe('t65.8');
    expect(spot?.getAttribute('data-target-id')).toBe('T65.8');
    expect(spot?.getAttribute('role')).toBe('button');
    expect(spot?.getAttribute('tabindex')).toBe('0');
  });

  it('leaves names without a target id plain', () => {
    const { container } = tree([
      { name: 'jevons-po' },
      { name: 'jv-compact-a7a1dc5e', parent: 'jevons-po' },
    ]);
    expect(container.querySelector('.target-hotspot')).toBeNull();
    const names = [...container.querySelectorAll('.agent-name')].map((el) => el.textContent);
    expect(names).toEqual(['jevons-po', 'jv-compact-a7a1dc5e']);
  });

  it('clicking the hotspot bubbles to row selection and opens the hovercard', () => {
    const selected: string[] = [];
    const { container } = tree([{ name: 'ge-t65.8-cancel-implement' }], [t658], (n) => selected.push(n));
    const spot = container.querySelector('.target-hotspot')!;
    fireEvent.click(spot);
    expect(selected).toEqual(['ge-t65.8-cancel-implement']);
    const tip = container.querySelector('.instant-tip-show');
    expect(tip).toBeTruthy();
    expect(tip?.textContent || '').toMatch(/Cancel implement/);
  });

  it('hovering the hotspot opens the same InstantTip hovercard', () => {
    const { container } = tree([{ name: 'T1014' }], [{ id: 'T1014', name: 'Whole-name seat', status: 'identified' }]);
    const spot = container.querySelector('.target-hotspot')!;
    expect(spot.textContent).toBe('T1014');
    expect(spot.getAttribute('data-target-id')).toBe('T1014');
    fireEvent.pointerEnter(spot);
    const tip = container.querySelector('.instant-tip-show');
    expect(tip).toBeTruthy();
    expect(tip?.textContent || '').toMatch(/Whole-name seat/);
  });

  it('Enter on the hotspot is keyboard activation', () => {
    const { container } = tree([{ name: 'ge-t65.8-cancel-implement' }]);
    const spot = container.querySelector('.target-hotspot')!;
    fireEvent.keyDown(spot, { key: 'Enter' });
    expect(container.querySelector('.instant-tip-show')).toBeTruthy();
  });

  it('off-frontier ids resolve through fetchFrontierTarget', async () => {
    const achieved: FrontierRow = {
      id: 'T999',
      name: 'Closed off-frontier',
      status: 'achieved',
      acceptance: ['Fetched, not frontier-stub'],
    };
    const prev = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const u = String(input);
      if (u.includes('/api/frontier/target') && u.includes('T999')) {
        return new Response(JSON.stringify({ available: true, found: true, target: achieved }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(JSON.stringify({ available: true, found: false }), { status: 404 });
    }) as typeof fetch;
    try {
      const { container } = tree([{ name: 'jv-t999-missing' }], []);
      fireEvent.pointerEnter(container.querySelector('.target-hotspot')!);
      await waitFor(() => {
        const tip = container.querySelector('.instant-tip-show')?.textContent || '';
        expect(tip).toMatch(/Closed off-frontier/);
        expect(tip).toMatch(/Fetched, not frontier-stub/);
      });
    } finally {
      globalThis.fetch = prev;
    }
  });
});

it('seat target scope follows workdir rather than seat name prefix (T1056)', () => {
  const { container } = render(
    <FrontierRowsContext.Provider value={[{ id: 'T177', name: 'Jevons collision', status: 'identified' }]}>
      <AgentTree agents={[{ name: 'jv-t177-worker', workdir: '/work/github.com/marcelocantos/claudia' }]} selected="" onSelect={() => {}} />
    </FrontierRowsContext.Provider>,
  );
  const spot = container.querySelector('.target-hotspot');
  expect(spot?.getAttribute('data-target-repo')).toBe('github.com/marcelocantos/claudia');
  expect(spot?.getAttribute('data-target-id')).toBe('T177');
});
