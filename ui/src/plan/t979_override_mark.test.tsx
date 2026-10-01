// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PlanUsageBar } from '../components/PlanUsageBar';
import { OVERRIDE_DEST_BANDS, overrideMark, overrideTipHeading } from './overrideMark';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const cockpitCSS = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '..', 'cockpit.css'), 'utf8');

/** The CSS rule whose selector is exactly `#plan-ticker <selector>`. */
function ruleFor(selector: string): string {
  const needle = '#plan-ticker ' + selector;
  const at = cockpitCSS.indexOf(needle + ' {');
  if (at < 0) throw new Error('no cockpit.css rule for ' + needle);
  return cockpitCSS.slice(at, cockpitCSS.indexOf('}', at));
}

async function renderOverride(band: string) {
  const reason = 'override ' + band;
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    if (input === '/api/plan-usage') {
      return { ok: true, json: async () => ({ backends: [{
        provider: 'claude', status: 'available', override: { band, reason },
        windows: [{ name: 'weekly', used_percent: 50, remaining_percent: 50, band: 'ok' }],
      }] }) };
    }
    if (input === '/api/plan-usage/decisions') return { ok: true, json: async () => [] };
    return { ok: true, json: async () => ({ plans: [] }) };
  }));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(<QueryClientProvider client={client}><PlanUsageBar /></QueryClientProvider>);
  const mark = await screen.findByLabelText('Override: ' + reason);
  return { mark, container };
}

// 🎯T979: a forced band is painted as that band; anything else is a block.
describe('overrideMark', () => {
  it('reads ok, under and locked as forced bands', () => {
    for (const band of OVERRIDE_DEST_BANDS) {
      expect(overrideMark({ band, reason: 'r' })).toEqual({ kind: 'band', band });
    }
  });
  it('reads every other band as an outright block', () => {
    for (const band of ['exhausted', 'hot', 'ahead', 'unpublished', 'Exhausted ', '']) {
      expect(overrideMark({ band, reason: 'r' }).kind).toBe('block');
    }
  });
  it('says blocked outright on the tip for a block, and the band for a forced band', () => {
    expect(overrideTipHeading('grok', { band: 'exhausted', reason: 'r' })).toBe('grok blocked outright by override (exhausted)');
    expect(overrideTipHeading('claude', { band: 'under', reason: 'r' })).toBe('claude shown under by override');
  });
});

describe('the override mark on the plan box', () => {
  // The rendered mark names its shape and band; cockpit.css keys the colour
  // on exactly those, so the two checks together pin colour to shape.
  it.each([
    ['ok', 'var(--green)'],
    ['under', 'var(--plan-under)'],
    ['locked', 'var(--plan-locked)'],
  ])('paints a forced %s override in that band\'s own colour', async (band, colour) => {
    const { mark } = await renderOverride(band);
    expect(mark.classList.contains('plan-override-band')).toBe(true);
    expect(mark.classList.contains('plan-override-block')).toBe(false);
    expect(mark.getAttribute('data-band')).toBe(band);
    const rule = ruleFor('.plan-override-band[data-band="' + band + '"]');
    expect(rule).toContain('background: ' + colour);
  });

  // 🎯T982: the block is a prohibition sign (ring + top-left-to-bottom-right
  // diagonal, the ⊘ family), not a ? on a black dot and not a stop sign.
  it('draws an outright block as a ⊘ prohibition sign in the text colour, on no dot and in no band colour', async () => {
    const { mark } = await renderOverride('exhausted');
    expect(mark.classList.contains('plan-override-block')).toBe(true);
    expect(mark.classList.contains('plan-override-band')).toBe(false);
    expect(mark.textContent).toBe('');
    const svg = mark.querySelector('svg.plan-override-ban');
    expect(svg).not.toBeNull();
    expect(svg!.getAttribute('aria-hidden')).toBe('true');
    const ring = svg!.querySelector('circle')!;
    const bar = svg!.querySelector('line')!;
    expect(ring.getAttribute('fill')).toBe('none');
    expect(ring.getAttribute('stroke')).toBe('currentColor');
    expect(bar.getAttribute('stroke')).toBe('currentColor');
    // The diagonal runs top-left to bottom-right through the ring's centre.
    const [x1, y1, x2, y2] = ['x1', 'y1', 'x2', 'y2'].map((a) => Number(bar.getAttribute(a)));
    const cx = Number(ring.getAttribute('cx'));
    const cy = Number(ring.getAttribute('cy'));
    expect(x1).toBeLessThan(x2);
    expect(y1).toBeLessThan(y2);
    expect(x2 - x1).toBeCloseTo(y2 - y1);
    expect((x1 + x2) / 2).toBeCloseTo(cx);
    expect((y1 + y2) / 2).toBeCloseTo(cy);
    // The ink is the theme's text colour; no dot behind it, no band colour on it.
    const rule = ruleFor('.plan-override-block');
    expect(rule).toContain('color: var(--text)');
    expect(rule).not.toMatch(/background/);
    expect(rule).not.toMatch(/--green|--plan-under|--plan-locked|--amber|--red/);
    // The base rule carries no colour of its own: an unmatched shape paints nothing green.
    expect(ruleFor('.plan-override')).not.toMatch(/background/);
  });

  it('keeps the ? on a forced band: only the block changed shape', async () => {
    const { mark } = await renderOverride('under');
    expect(mark.textContent).toBe('?');
    expect(mark.querySelector('svg')).toBeNull();
  });

  it('renders a block and a forced ok with different shape and band, never the same visual', async () => {
    const block = await renderOverride('locked');
    const lockedClass = block.mark.className;
    const lockedBand = block.mark.getAttribute('data-band');
    cleanup();
    const ok = await renderOverride('ok');
    expect(ok.mark.className).toBe(lockedClass);
    expect(ok.mark.getAttribute('data-band')).not.toBe(lockedBand);
    cleanup();
    const blocked = await renderOverride('exhausted');
    expect(blocked.mark.className).not.toBe(ok.mark.className);
  });

  it('says blocked outright on the tip card while the pointer is on a block mark', async () => {
    const { mark, container } = await renderOverride('exhausted');
    fireEvent.pointerEnter(container.querySelector('[data-instant-tip-host]')!);
    fireEvent.pointerEnter(mark);
    expect(document.body.textContent).toContain('claude blocked outright by override (exhausted)');
    expect(document.body.textContent).not.toContain('shown exhausted by override');
  });
});
