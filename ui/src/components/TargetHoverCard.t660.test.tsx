// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, render } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

// 🎯T660: a target hovercard's dependency minigraph stays a painted mermaid
// diagram for the life of the card — a re-render or remount of the same card
// under transcript activity never drops it back to the raw fence.

type MermaidStub = {
  initialize: () => void;
  render: (id: string, src: string) => Promise<{ svg: string }>;
  renders: number;
};

const stub: MermaidStub = {
  renders: 0,
  initialize: () => {},
  render: async (id: string) => {
    stub.renders++;
    // Real mermaid is async; so is this, so the fence is visible between
    // mount and paint exactly as it is in the cockpit.
    await new Promise((r) => setTimeout(r, 5));
    return { svg: '<svg data-t660="' + id + '"><g>deps</g></svg>' };
  },
};

const md = [
  '**🎯T659** — The clean-checkout web gate',
  '',
  '**Dependencies**',
  '',
  '```mermaid',
  'graph LR',
  '  T659["T659 · The clean-checkout web gate…"]',
  '  style T659 stroke-width:2px',
  '```',
].join('\n');

let TargetHoverCard: typeof import('./TargetHoverCard').TargetHoverCard;
let resetPaintedHoverCards: typeof import('./TargetHoverCard').resetPaintedHoverCards;

beforeAll(async () => {
  (window as unknown as { mermaid: MermaidStub }).mermaid = stub;
  const mod = await import('./TargetHoverCard');
  TargetHoverCard = mod.TargetHoverCard;
  resetPaintedHoverCards = mod.resetPaintedHoverCards;
});

async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 20));
  });
}

describe('TargetHoverCard mermaid stays painted (🎯T660)', () => {
  it('first mount paints the fence; a remount of the same card starts from the diagram', async () => {
    resetPaintedHoverCards();
    const first = render(<TargetHoverCard markdown={md} id="T659" />);
    // Before the async paint lands the fence is what there is.
    expect(first.container.querySelector('code.language-mermaid')).not.toBeNull();
    await settle();
    expect(first.container.querySelector('.mermaid-diagram svg')).not.toBeNull();
    expect(first.container.querySelector('code.language-mermaid')).toBeNull();
    const rendersAfterFirst = stub.renders;

    // The cockpit under load: the tip is re-pinned and the card remounted
    // with the same markdown. It must open on the diagram, synchronously.
    first.unmount();
    const second = render(<TargetHoverCard markdown={md} id="T659" />);
    expect(second.container.querySelector('.mermaid-diagram svg')).not.toBeNull();
    expect(second.container.querySelector('code.language-mermaid')).toBeNull();
    await settle();
    expect(second.container.querySelector('.mermaid-diagram svg')).not.toBeNull();
    expect(second.container.querySelector('code.language-mermaid')).toBeNull();
    expect(stub.renders).toBe(rendersAfterFirst);
  });

  it('a re-render with a fresh but identical props object keeps the diagram', async () => {
    resetPaintedHoverCards();
    const view = render(<TargetHoverCard markdown={md} id="T659" />);
    await settle();
    expect(view.container.querySelector('.mermaid-diagram svg')).not.toBeNull();
    for (let i = 0; i < 5; i++) {
      view.rerender(<TargetHoverCard markdown={String(md)} id="T659" name={'tick ' + i} />);
      expect(view.container.querySelector('code.language-mermaid')).toBeNull();
      expect(view.container.querySelector('.mermaid-diagram svg')).not.toBeNull();
    }
  });
});
