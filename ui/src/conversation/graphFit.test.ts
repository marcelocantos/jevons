// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  MIN_LEGIBLE_LABEL_PX,
  assessMicroStripLayout,
  assessPaneInkCover,
  classifyFetchFailureKind,
  computeContainScale,
  packBoxesIntoShelfWidth,
  placementSvgAspectMatchesNatural,
  planFrontierGraphFit,
  planSingleGraphScaleToFill,
  productFetchFailureFromError,
} from './graphFit';

const OWNER_STRIP = { w: 3000, h: 200 };
const OWNER_PANE = { w: 1600, h: 850 };

describe('T294 graph fit', () => {
  it('contain-scaling a wide strip is an illegible micro strip', () => {
    const contain = computeContainScale(OWNER_STRIP.w, OWNER_STRIP.h, OWNER_PANE.w, OWNER_PANE.h);
    const strip = assessMicroStripLayout({
      paneW: OWNER_PANE.w,
      paneH: OWNER_PANE.h,
      svgDisplayW: OWNER_STRIP.w * contain,
      svgDisplayH: OWNER_STRIP.h * contain,
      scale: contain,
    });
    expect(strip.isMicroStrip).toBe(true);
  });

  it('a graph that already fits keeps its contain scale', () => {
    const plan = planSingleGraphScaleToFill({
      svgW: 800,
      svgH: 600,
      paneW: OWNER_PANE.w,
      paneH: OWNER_PANE.h,
      padding: 0,
    });
    expect(plan.floored).toBe(false);
    expect(plan.scale).toBeGreaterThan(1);
    expect(plan.overflowX).toBe(false);
    expect(plan.overflowY).toBe(false);
  });

  it('multi-component pack reflows at the floor and fills the pane', () => {
    const boxes = [{ w: OWNER_STRIP.w, h: OWNER_STRIP.h, id: 'c0' }];
    for (let i = 1; i < 7; i++) boxes.push({ w: 600, h: 220, id: 'c' + i });
    const plan = planFrontierGraphFit({
      boxes,
      paneW: OWNER_PANE.w,
      paneH: OWNER_PANE.h,
      gap: 12,
      chromeH: 48,
    });
    expect(plan.mode).toBe('reflow-readable');
    expect(plan.reflowed).toBe(true);
    expect(plan.legible).toBe(true);
    expect(plan.labelPx).toBeGreaterThanOrEqual(MIN_LEGIBLE_LABEL_PX);
    expect(plan.fillsPane).toBe(true);
    expect(plan.placements.length).toBe(7);
    for (const p of plan.placements) {
      expect(placementSvgAspectMatchesNatural(p, 1e-6)).toBe(true);
    }
    const packInk = assessPaneInkCover(
      { paneW: OWNER_PANE.w, paneH: OWNER_PANE.h },
      plan.placements.map((p) => ({ w: p.svgDisplayW, h: p.svgDisplayH })),
    );
    const containScale = computeContainScale(OWNER_STRIP.w, OWNER_STRIP.h, OWNER_PANE.w, OWNER_PANE.h);
    const stripInk = assessPaneInkCover(
      { paneW: OWNER_PANE.w, paneH: OWNER_PANE.h },
      [{ w: OWNER_STRIP.w * containScale, h: OWNER_STRIP.h * containScale }],
    );
    expect(stripInk.ok).toBe(false);
    expect(packInk.cover).toBeGreaterThan(stripInk.cover);
  });

  it('pack keeps contain scale when it is already readable', () => {
    const plan = planFrontierGraphFit({
      boxes: [
        { w: 500, h: 400, id: 'a' },
        { w: 480, h: 380, id: 'b' },
      ],
      paneW: OWNER_PANE.w,
      paneH: OWNER_PANE.h,
      gap: 12,
      chromeH: 48,
    });
    expect(plan.mode).toBe('pack-scale-to-fill');
    expect(plan.reflowed).toBe(false);
    expect(plan.fillsPane).toBe(true);
    expect(plan.labelPx).toBeGreaterThanOrEqual(MIN_LEGIBLE_LABEL_PX);
  });

  it('shelf pack honours an explicit bin width', () => {
    const packed = packBoxesIntoShelfWidth(
      [{ w: 100, h: 50 }, { w: 100, h: 50 }, { w: 100, h: 50 }],
      { shelfWidth: 220, gap: 10 },
    );
    expect(packed.rowCount).toBe(2);
    expect(packed.placements.length).toBe(3);
    const forced = packBoxesIntoShelfWidth([{ w: 900, h: 50 }], { shelfWidth: 100 });
    expect(forced.shelfWidth).toBe(900);
  });

  it('bullseye panic classifies loud, never as an empty graph', () => {
    const msg = 'bullseye open: exit status 101 — panic graph.rs:704';
    expect(classifyFetchFailureKind(msg, null)).toBe('panic');
    expect(classifyFetchFailureKind('bullseye: exit status 2', null)).toBe('server');
    expect(classifyFetchFailureKind('Failed to fetch', null)).toBe('network');
    expect(classifyFetchFailureKind('boom', 500)).toBe('http');
    const view = productFetchFailureFromError({ message: msg }, { resource: 'Unachieved graph' });
    expect(view.kind).toBe('panic');
    expect(view.status).toMatch(/Backend panic/);
    expect(view.bodyHtml).toMatch(/graph\.rs:704/);
    expect(view.bodyHtml).toMatch(/data-mvp-fetch-error="1"/);
    expect(view.bodyHtml).toMatch(/data-mvp-error-kind="panic"/);
    expect(view.bodyHtml).not.toMatch(/data-mvp-empty/);
    expect(view.bodyHtml).not.toMatch(/No graph loaded/);
    expect(view.bodyHtml).toMatch(/restart-daily/);
  });
});
