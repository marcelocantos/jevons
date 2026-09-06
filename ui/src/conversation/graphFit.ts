// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Frontier Graph pack/fit (vanilla mermaid_actions.js 🎯T268/T276/T277/T294). */

export const PACK_BLOCK_CHROME_H = 48;
export const MERMAID_LABEL_FONT_PX = 16;
export const MIN_LEGIBLE_LABEL_PX = 11;

export const PRODUCT_FETCH_RECOVERY_HINT =
  'Rebuild jevonsd if needed, then run scripts/restart-daily-jevonsd.sh and hard-reload the UI.';
export const PRODUCT_FETCH_RECOVERY_SHORT = 'rebuild / restart-daily';

export type SizeBox = { w: number; h: number; id?: string };

export type ShelfPlacement = {
  i: number;
  id?: string;
  x: number;
  y: number;
  w: number;
  h: number;
  row: number;
};

export type DisplayPlacement = ShelfPlacement & {
  naturalW: number;
  naturalH: number;
  displayX: number;
  displayY: number;
  displayW: number;
  displayH: number;
  svgDisplayW: number;
  svgDisplayH: number;
};

export type PackResult = {
  placements: ShelfPlacement[];
  compositeW: number;
  compositeH: number;
  shelfWidth: number;
  rowCount: number;
};

export type GraphFitPlan = {
  mode: 'pack-scale-to-fill' | 'reflow-readable' | 'scale-to-fill' | 'skip';
  scale: number;
  floorScale?: number;
  containScale?: number;
  labelPx: number;
  legible: boolean;
  reflowed?: boolean;
  floored?: boolean;
  overflowX: boolean;
  overflowY: boolean;
  compositeW?: number;
  compositeH?: number;
  displayW: number;
  displayH: number;
  fillsPane: boolean;
  chromeH?: number;
  placements: DisplayPlacement[];
};

export type FetchFailureKind = 'http' | 'panic' | 'server' | 'network' | 'unknown';

export type FetchFailureView = {
  title: string;
  status: string;
  bodyHtml: string;
  httpStatus: number | null;
  kind: FetchFailureKind;
  recoveryHint: string;
};

export function escapeHtml(s: unknown): string {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export function stripMermaidFence(text: unknown): string {
  let s = String(text == null ? '' : text).replace(/^\uFEFF/, '').trim();
  if (!s) return '';
  const fenced = s.match(/^```(?:mermaid)?\s*\r?\n([\s\S]*?)\r?\n```\s*$/i);
  if (fenced) return String(fenced[1] || '').trim();
  const lead = s.match(/^```(?:mermaid)?\s*\r?\n([\s\S]*)$/i);
  if (lead) {
    return String(lead[1] || '').replace(/\r?\n```\s*$/, '').trim();
  }
  return s;
}

export function positiveNumber(n: unknown): number {
  const x = typeof n === 'number' ? n : parseFloat(String(n));
  return isFinite(x) && x > 0 ? x : 0;
}

export function legibilityFloorScale(opts?: { naturalFontPx?: number; minLabelPx?: number }): number {
  const natural = positiveNumber(opts?.naturalFontPx) || MERMAID_LABEL_FONT_PX;
  const floor = positiveNumber(opts?.minLabelPx) || MIN_LEGIBLE_LABEL_PX;
  if (!natural) return 1;
  return floor / natural;
}

export function effectiveLabelPx(scale: unknown, naturalFontPx?: unknown): number {
  const s = positiveNumber(scale);
  const natural = positiveNumber(naturalFontPx) || MERMAID_LABEL_FONT_PX;
  return s * natural;
}

export function assessGraphLegibility(metrics: {
  scale?: number;
  naturalFontPx?: number;
  minLabelPx?: number;
}): { ok: boolean; labelPx: number; minLabelPx: number; floorScale: number; mode: string } {
  const natural = positiveNumber(metrics.naturalFontPx) || MERMAID_LABEL_FONT_PX;
  const floorPx = positiveNumber(metrics.minLabelPx) || MIN_LEGIBLE_LABEL_PX;
  const scale = positiveNumber(metrics.scale);
  const labelPx = effectiveLabelPx(scale, natural);
  return {
    ok: labelPx > 0 && labelPx >= floorPx - 1e-9,
    labelPx,
    minLabelPx: floorPx,
    floorScale: floorPx / natural,
    mode: 'label-legibility',
  };
}

export function assessMicroStripLayout(
  metrics: {
    paneW?: number;
    paneH?: number;
    svgDisplayW?: number;
    svgDisplayH?: number;
    scale?: number;
    naturalFontPx?: number;
    minLabelPx?: number;
  },
  opts?: { majorCover?: number; minorCover?: number },
): {
  isMicroStrip: boolean;
  stripShape?: boolean;
  coverW: number;
  coverH: number;
  legible: ReturnType<typeof assessGraphLegibility>;
  mode: string;
} {
  const majorMin = opts?.majorCover != null ? positiveNumber(opts.majorCover) : 0.9;
  const minorMax = opts?.minorCover != null ? positiveNumber(opts.minorCover) : 0.35;
  const paneW = positiveNumber(metrics.paneW);
  const paneH = positiveNumber(metrics.paneH);
  const dw = positiveNumber(metrics.svgDisplayW);
  const dh = positiveNumber(metrics.svgDisplayH);
  const legible = assessGraphLegibility(metrics);
  if (!paneW || !paneH || !dw || !dh) {
    return { isMicroStrip: false, coverW: 0, coverH: 0, legible, mode: 'micro-strip' };
  }
  const coverW = dw / paneW;
  const coverH = dh / paneH;
  const major = Math.max(coverW, coverH);
  const minor = Math.min(coverW, coverH);
  const stripShape = major >= majorMin && minor < minorMax;
  return {
    isMicroStrip: stripShape && !legible.ok,
    stripShape,
    coverW,
    coverH,
    legible,
    mode: 'micro-strip',
  };
}

export function assessSingleGraphPaneCover(
  metrics: { paneW?: number; paneH?: number; svgDisplayW?: number; svgDisplayH?: number },
  opts?: { minCover?: number },
): { ok: boolean; cover: number; mode: string } {
  const minCover = opts?.minCover != null ? positiveNumber(opts.minCover) : 0.75;
  const paneW = positiveNumber(metrics.paneW);
  const paneH = positiveNumber(metrics.paneH);
  const svgW = positiveNumber(metrics.svgDisplayW);
  const svgH = positiveNumber(metrics.svgDisplayH);
  if (!paneW || !paneH || !svgW || !svgH) {
    return { ok: false, cover: 0, mode: 'single-pane-cover' };
  }
  const cover = Math.max(svgW / paneW, svgH / paneH);
  return { ok: cover >= minCover, cover, mode: 'single-pane-cover' };
}

export function assessPaneInkCover(
  pane: { paneW?: number; paneH?: number },
  inkBoxes: Array<{ w?: number; h?: number }>,
  opts?: { minCover?: number },
): { ok: boolean; cover: number; inkArea: number; paneArea: number; mode: string } {
  const minCover = opts?.minCover != null ? positiveNumber(opts.minCover) : 0.25;
  const paneW = positiveNumber(pane.paneW);
  const paneH = positiveNumber(pane.paneH);
  let ink = 0;
  for (const b of inkBoxes || []) {
    ink += positiveNumber(b.w) * positiveNumber(b.h);
  }
  const paneArea = paneW * paneH;
  const cover = paneArea > 0 ? Math.min(1, ink / paneArea) : 0;
  return { ok: cover >= minCover, cover, inkArea: ink, paneArea, mode: 'pane-ink-cover' };
}

export function placementSvgAspectMatchesNatural(placement: Partial<DisplayPlacement>, eps?: number): boolean {
  const nw = positiveNumber(placement.naturalW != null ? placement.naturalW : placement.w);
  const nh = positiveNumber(placement.naturalH != null ? placement.naturalH : placement.h);
  const dw = positiveNumber(placement.svgDisplayW != null ? placement.svgDisplayW : placement.displayW);
  const dh = positiveNumber(placement.svgDisplayH != null ? placement.svgDisplayH : placement.displayH);
  if (!nw || !nh || !dw || !dh) return false;
  const tol = eps != null && isFinite(eps) ? Math.abs(eps) : 1e-6;
  return Math.abs(dw / dh - nw / nh) <= tol;
}

export function parseSvgNaturalSize(svg: {
  width?: unknown;
  height?: unknown;
  viewBox?: unknown;
  getAttribute?: (name: string) => string | null;
} | null): { w: number; h: number } {
  if (!svg || typeof svg !== 'object') return { w: 0, h: 0 };
  let w = 0;
  let h = 0;
  if (typeof svg.getAttribute === 'function') {
    w = positiveNumber(svg.getAttribute('width'));
    h = positiveNumber(svg.getAttribute('height'));
    if ((!w || !h) && svg.getAttribute('viewBox')) {
      const vb = String(svg.getAttribute('viewBox') || '').trim().split(/[\s,]+/);
      if (vb.length >= 4) {
        if (!w) w = positiveNumber(vb[2]);
        if (!h) h = positiveNumber(vb[3]);
      }
    }
  } else {
    w = positiveNumber(svg.width);
    h = positiveNumber(svg.height);
    if ((!w || !h) && svg.viewBox) {
      const vb = String(svg.viewBox).trim().split(/[\s,]+/);
      if (vb.length >= 4) {
        if (!w) w = positiveNumber(vb[2]);
        if (!h) h = positiveNumber(vb[3]);
      }
    }
  }
  return { w, h };
}

export function computeContainScale(svgW: unknown, svgH: unknown, paneW: unknown, paneH: unknown): number {
  const sw = positiveNumber(svgW);
  const sh = positiveNumber(svgH);
  const pw = positiveNumber(paneW);
  const ph = positiveNumber(paneH);
  if (!sw || !sh || !pw || !ph) return 1;
  return Math.min(pw / sw, ph / sh);
}

function shelfItems(boxes: SizeBox[]): Array<SizeBox & { i: number }> {
  const items: Array<SizeBox & { i: number }> = [];
  (boxes || []).forEach((b, i) => {
    const w = positiveNumber(b?.w);
    const h = positiveNumber(b?.h);
    if (!w || !h) return;
    items.push({ i, id: b.id, w, h });
  });
  return items;
}

export function packBoxesIntoShelfWidth(
  boxes: SizeBox[],
  opts?: { shelfWidth?: number; gap?: number },
): PackResult {
  const gap = opts?.gap != null ? Math.max(0, positiveNumber(opts.gap) || 0) : 12;
  const items = shelfItems(boxes);
  if (!items.length) {
    return { placements: [], compositeW: 0, compositeH: 0, shelfWidth: 0, rowCount: 0 };
  }
  let maxW = 0;
  for (const it of items) {
    if (it.w > maxW) maxW = it.w;
  }
  const shelfWidth = Math.max(maxW, positiveNumber(opts?.shelfWidth));
  items.sort((a, b) => (b.h !== a.h ? b.h - a.h : a.i - b.i));
  const placements: ShelfPlacement[] = [];
  let cursorY = 0;
  let shelfH = 0;
  let cursorX = 0;
  let usedW = 0;
  let row = 0;
  for (const it of items) {
    if (cursorX > 0 && cursorX + it.w > shelfWidth + 1e-9) {
      cursorY += shelfH + gap;
      cursorX = 0;
      shelfH = 0;
      row += 1;
    }
    placements.push({ i: it.i, id: it.id, x: cursorX, y: cursorY, w: it.w, h: it.h, row });
    cursorX += it.w + gap;
    if (it.h > shelfH) shelfH = it.h;
    const rowRight = cursorX - gap;
    if (rowRight > usedW) usedW = rowRight;
  }
  placements.sort((a, b) => a.i - b.i);
  return {
    placements,
    compositeW: Math.max(usedW, maxW),
    compositeH: cursorY + shelfH,
    shelfWidth,
    rowCount: row + 1,
  };
}

export function packBoxesIntoPaneAspect(
  boxes: SizeBox[],
  opts?: { paneW?: number; paneH?: number; gap?: number },
): PackResult {
  const paneW = positiveNumber(opts?.paneW);
  const paneH = positiveNumber(opts?.paneH);
  const items = shelfItems(boxes);
  if (!items.length) {
    return { placements: [], compositeW: 0, compositeH: 0, shelfWidth: 0, rowCount: 0 };
  }
  let maxW = 0;
  let area = 0;
  for (const it of items) {
    if (it.w > maxW) maxW = it.w;
    area += it.w * it.h;
  }
  let shelfWidth = maxW;
  if (paneW > 0 && paneH > 0) {
    const aspect = paneW / paneH;
    const idealW = Math.sqrt(area * aspect);
    shelfWidth = Math.max(maxW, idealW);
    if (paneW >= maxW) shelfWidth = Math.min(Math.max(shelfWidth, maxW), Math.max(paneW, maxW));
    else shelfWidth = maxW;
  }
  return packBoxesIntoShelfWidth(boxes, { shelfWidth, gap: opts?.gap });
}

export function placePackedRows(
  packed: { placements?: ShelfPlacement[] },
  opts?: { scale?: number; chromeH?: number; gap?: number },
): { placements: DisplayPlacement[]; displayW: number; displayH: number } {
  const scale = positiveNumber(opts?.scale) || 1;
  const chromeH = Math.max(0, positiveNumber(opts?.chromeH) || 0);
  const gap = Math.max(0, positiveNumber(opts?.gap) || 0);
  const src = Array.isArray(packed?.placements) ? packed.placements : [];
  const byRow: Record<number, ShelfPlacement[]> = {};
  const rowOrder: number[] = [];
  for (const p of src) {
    const r = p.row != null ? p.row : 0;
    if (!byRow[r]) {
      byRow[r] = [];
      rowOrder.push(r);
    }
    byRow[r].push(p);
  }
  rowOrder.sort((a, b) => a - b);
  const placements: DisplayPlacement[] = [];
  let cursorY = 0;
  let usedW = 0;
  for (let ri = 0; ri < rowOrder.length; ri++) {
    const rowPls = byRow[rowOrder[ri]];
    let rowSvgH = 0;
    for (const p of rowPls) {
      if (p.h > rowSvgH) rowSvgH = p.h;
    }
    for (const p of rowPls) {
      const svgDisplayW = p.w * scale;
      const svgDisplayH = p.h * scale;
      const displayX = p.x * scale;
      const right = displayX + svgDisplayW;
      if (right > usedW) usedW = right;
      placements.push({
        ...p,
        naturalW: p.w,
        naturalH: p.h,
        displayX,
        displayY: cursorY,
        displayW: svgDisplayW,
        displayH: svgDisplayH + chromeH,
        svgDisplayW,
        svgDisplayH,
      });
    }
    cursorY += rowSvgH * scale + chromeH;
    if (ri < rowOrder.length - 1) cursorY += gap * scale;
  }
  placements.sort((a, b) => a.i - b.i);
  return { placements, displayW: usedW, displayH: cursorY };
}

export function planMultiDiagramPackScaleToFill(opts: {
  boxes?: SizeBox[];
  paneW?: number;
  paneH?: number;
  padding?: number;
  gap?: number;
  chromeH?: number;
}): GraphFitPlan {
  const pad = opts.padding != null ? Math.max(0, positiveNumber(opts.padding) || 0) : 0;
  const paneW = Math.max(0, positiveNumber(opts.paneW) - pad);
  const paneH = Math.max(0, positiveNumber(opts.paneH) - pad);
  const gap = opts.gap != null ? Math.max(0, positiveNumber(opts.gap) || 0) : 12;
  const chromeH = opts.chromeH != null ? Math.max(0, positiveNumber(opts.chromeH) || 0) : PACK_BLOCK_CHROME_H;
  const packed = packBoxesIntoPaneAspect(opts.boxes || [], { paneW, paneH, gap });
  if (!packed.placements.length || !packed.compositeW || !packed.compositeH || !paneW || !paneH) {
    return {
      mode: 'skip',
      scale: 1,
      labelPx: 0,
      legible: false,
      overflowX: false,
      overflowY: false,
      compositeW: packed.compositeW || 0,
      compositeH: packed.compositeH || 0,
      displayW: packed.compositeW || 0,
      displayH: packed.compositeH || 0,
      fillsPane: false,
      chromeH,
      placements: [],
    };
  }
  const cW = packed.compositeW;
  const cH = packed.compositeH;
  const nRows = packed.rowCount > 0 ? packed.rowCount : 1;
  const scaleW = paneW / cW;
  let scaleH: number;
  if (chromeH > 0 && nRows * chromeH < paneH) {
    scaleH = (paneH - nRows * chromeH) / cH;
  } else if (chromeH <= 0) {
    scaleH = paneH / cH;
  } else {
    scaleH = paneH / (cH + nRows * chromeH);
  }
  let scale = Math.min(scaleW, scaleH);
  if (!(scale > 0) || !isFinite(scale)) scale = 1;
  const laid = placePackedRows(packed, { scale, chromeH, gap });
  const displayW = laid.displayW > 0 ? laid.displayW : cW * scale;
  const displayH = laid.displayH;
  return {
    mode: 'pack-scale-to-fill',
    scale,
    labelPx: 0,
    legible: false,
    overflowX: displayW > paneW + 1,
    overflowY: displayH > paneH + 1,
    compositeW: cW,
    compositeH: cH,
    displayW,
    displayH,
    fillsPane: Math.max(displayW / paneW, displayH / paneH) >= 0.95,
    chromeH,
    placements: laid.placements,
  };
}

export function planFrontierGraphFit(opts: {
  boxes?: SizeBox[];
  paneW?: number;
  paneH?: number;
  padding?: number;
  gap?: number;
  chromeH?: number;
  naturalFontPx?: number;
  minLabelPx?: number;
}): GraphFitPlan {
  const pad = opts.padding != null ? Math.max(0, positiveNumber(opts.padding) || 0) : 0;
  const paneW = Math.max(0, positiveNumber(opts.paneW) - pad);
  const paneH = Math.max(0, positiveNumber(opts.paneH) - pad);
  const gap = opts.gap != null ? Math.max(0, positiveNumber(opts.gap) || 0) : 12;
  const chromeH = opts.chromeH != null ? Math.max(0, positiveNumber(opts.chromeH) || 0) : PACK_BLOCK_CHROME_H;
  const naturalFontPx = positiveNumber(opts.naturalFontPx) || MERMAID_LABEL_FONT_PX;
  const minLabelPx = positiveNumber(opts.minLabelPx) || MIN_LEGIBLE_LABEL_PX;
  const floorScale = legibilityFloorScale({ naturalFontPx, minLabelPx });
  const fit = planMultiDiagramPackScaleToFill({
    boxes: opts.boxes || [],
    paneW,
    paneH,
    padding: 0,
    gap,
    chromeH,
  });
  if (fit.mode !== 'pack-scale-to-fill') {
    return {
      mode: 'skip',
      scale: 1,
      floorScale,
      labelPx: 0,
      legible: false,
      reflowed: false,
      overflowX: false,
      overflowY: false,
      compositeW: fit.compositeW || 0,
      compositeH: fit.compositeH || 0,
      displayW: fit.displayW || 0,
      displayH: fit.displayH || 0,
      fillsPane: false,
      chromeH,
      placements: [],
    };
  }
  if (fit.scale >= floorScale - 1e-9) {
    return {
      mode: 'pack-scale-to-fill',
      scale: fit.scale,
      floorScale,
      labelPx: effectiveLabelPx(fit.scale, naturalFontPx),
      legible: true,
      reflowed: false,
      overflowX: (fit.displayW || 0) > paneW + 1,
      overflowY: (fit.displayH || 0) > paneH + 1,
      compositeW: fit.compositeW,
      compositeH: fit.compositeH,
      displayW: fit.displayW,
      displayH: fit.displayH,
      fillsPane: fit.fillsPane,
      chromeH,
      placements: fit.placements,
    };
  }
  const scale = floorScale;
  const binW = scale > 0 ? paneW / scale : paneW;
  const packed = packBoxesIntoShelfWidth(opts.boxes || [], { shelfWidth: binW, gap });
  if (!packed.placements.length) {
    return {
      mode: 'skip',
      scale: 1,
      floorScale,
      labelPx: 0,
      legible: false,
      reflowed: false,
      overflowX: false,
      overflowY: false,
      compositeW: 0,
      compositeH: 0,
      displayW: 0,
      displayH: 0,
      fillsPane: false,
      chromeH,
      placements: [],
    };
  }
  const laid = placePackedRows(packed, { scale, chromeH, gap });
  const displayW = laid.displayW;
  const displayH = laid.displayH;
  return {
    mode: 'reflow-readable',
    scale,
    floorScale,
    labelPx: effectiveLabelPx(scale, naturalFontPx),
    legible: true,
    reflowed: true,
    overflowX: displayW > paneW + 1,
    overflowY: displayH > paneH + 1,
    compositeW: packed.compositeW,
    compositeH: packed.compositeH,
    displayW,
    displayH,
    fillsPane: paneW > 0 && paneH > 0 && Math.max(displayW / paneW, displayH / paneH) >= 0.95,
    chromeH,
    placements: laid.placements,
  };
}

export function planSingleGraphScaleToFill(opts: {
  svgW?: number;
  svgH?: number;
  paneW?: number;
  paneH?: number;
  padding?: number;
  diagramCount?: number;
  naturalFontPx?: number;
  minLabelPx?: number;
}): GraphFitPlan {
  const count = opts.diagramCount != null ? Math.floor(opts.diagramCount) : 1;
  if (count > 1) {
    return {
      mode: 'skip',
      scale: 1,
      labelPx: 0,
      legible: false,
      overflowX: false,
      overflowY: false,
      displayW: positiveNumber(opts.svgW),
      displayH: positiveNumber(opts.svgH),
      fillsPane: false,
      placements: [],
    };
  }
  const pad = opts.padding != null ? Math.max(0, positiveNumber(opts.padding) || 0) : 24;
  const paneW = Math.max(0, positiveNumber(opts.paneW) - pad);
  const paneH = Math.max(0, positiveNumber(opts.paneH) - pad);
  const svgW = positiveNumber(opts.svgW);
  const svgH = positiveNumber(opts.svgH);
  if (!svgW || !svgH || !paneW || !paneH) {
    return {
      mode: 'skip',
      scale: 1,
      labelPx: 0,
      legible: false,
      overflowX: false,
      overflowY: false,
      displayW: svgW,
      displayH: svgH,
      fillsPane: false,
      placements: [],
    };
  }
  const naturalFontPx = positiveNumber(opts.naturalFontPx) || MERMAID_LABEL_FONT_PX;
  const minLabelPx = positiveNumber(opts.minLabelPx) || MIN_LEGIBLE_LABEL_PX;
  const floorScale = legibilityFloorScale({ naturalFontPx, minLabelPx });
  const containScale = computeContainScale(svgW, svgH, paneW, paneH);
  const scale = Math.max(containScale, floorScale);
  const displayW = svgW * scale;
  const displayH = svgH * scale;
  return {
    mode: 'scale-to-fill',
    scale,
    containScale,
    floorScale,
    labelPx: effectiveLabelPx(scale, naturalFontPx),
    legible: true,
    floored: scale > containScale + 1e-9,
    overflowX: displayW > paneW + 1,
    overflowY: displayH > paneH + 1,
    displayW,
    displayH,
    fillsPane: Math.max(displayW / paneW, displayH / paneH) >= 0.95,
    placements: [],
  };
}

export function svgScaleToFillStyle(plan: Partial<GraphFitPlan>): { width: string; height: string; maxWidth: string; maxHeight: string } | null {
  if (
    (plan.mode !== 'scale-to-fill' && plan.mode !== 'pack-scale-to-fill' && plan.mode !== 'reflow-readable') ||
    !(positiveNumber(plan.displayW) > 0) ||
    !(positiveNumber(plan.displayH) > 0)
  ) {
    return null;
  }
  return {
    width: Math.round((plan.displayW as number) * 1000) / 1000 + 'px',
    height: Math.round((plan.displayH as number) * 1000) / 1000 + 'px',
    maxWidth: 'none',
    maxHeight: 'none',
  };
}

export function applySvgScaleToFill(
  svg: Element | null,
  plan: { mode: GraphFitPlan['mode']; scale: number; displayW: number; displayH: number },
): boolean {
  const style = svgScaleToFillStyle(plan);
  if (!svg || !style) return false;
  const el = svg as HTMLElement;
  if (el.style) {
    el.style.width = style.width;
    el.style.height = style.height;
    el.style.maxWidth = style.maxWidth;
    el.style.maxHeight = style.maxHeight;
  }
  svg.removeAttribute?.('width');
  svg.removeAttribute?.('height');
  svg.setAttribute?.('data-mvp-scale-fill', String(plan.scale));
  return true;
}

export function applyPackPlacement(
  block: HTMLElement | null,
  svg: Element | null,
  placement: DisplayPlacement,
  scale: number,
): boolean {
  if (!placement) return false;
  const s = positiveNumber(scale) || 1;
  const natW = positiveNumber(placement.naturalW != null ? placement.naturalW : placement.w);
  const natH = positiveNumber(placement.naturalH != null ? placement.naturalH : placement.h);
  let svgW = positiveNumber(placement.svgDisplayW);
  let svgH = positiveNumber(placement.svgDisplayH);
  if (!svgW || !svgH) {
    if (natW && natH) {
      svgW = natW * s;
      svgH = natH * s;
    } else {
      svgW = positiveNumber(placement.displayW);
      svgH = positiveNumber(placement.displayH);
    }
  }
  if (block?.style) {
    block.style.position = 'absolute';
    block.style.left = Math.round(placement.displayX * 1000) / 1000 + 'px';
    block.style.top = Math.round(placement.displayY * 1000) / 1000 + 'px';
    block.style.width = Math.round(placement.displayW * 1000) / 1000 + 'px';
    block.style.height = placement.displayH > 0 ? Math.round(placement.displayH * 1000) / 1000 + 'px' : 'auto';
    block.style.margin = '0';
    block.style.gridColumn = 'auto';
    block.style.overflow = 'hidden';
  }
  if (svg && svgW > 0 && svgH > 0) {
    applySvgScaleToFill(svg, { mode: 'scale-to-fill', scale: s, displayW: svgW, displayH: svgH });
  }
  if (block) {
    block.setAttribute('data-mvp-pack-placed', '1');
    block.setAttribute('data-mvp-pack-scale', String(s));
    if (svgW > 0 && svgH > 0) block.setAttribute('data-mvp-svg-aspect', String(svgW / svgH));
  }
  return true;
}

export function classifyFetchFailureKind(message: unknown, httpStatus: number | null): FetchFailureKind {
  const msg = String(message == null ? '' : message);
  if (/\bpanic(ked)?\b|\bfatal runtime\b|\bstack backtrace\b/i.test(msg)) return 'panic';
  if (httpStatus != null) return 'http';
  if (/failed to fetch|networkerror|network error|load failed|econnrefused/i.test(msg)) return 'network';
  if (/\bexit status\s+\d+|\bsignal:\s|\bexec\b.*\bnot found\b|\bexecutable file not found\b/i.test(msg)) {
    return 'server';
  }
  return msg.trim() ? 'server' : 'unknown';
}

export function productFetchFailureView(info: {
  resource?: string;
  status?: number;
  message?: string;
  kind?: string;
  recoveryHint?: string;
}): FetchFailureView {
  const resource = String(info.resource || 'Request').trim() || 'Request';
  let httpStatus: number | null = null;
  if (typeof info.status === 'number' && isFinite(info.status) && info.status > 0) {
    httpStatus = Math.floor(info.status);
  }
  let detail = String(info.message == null ? '' : info.message).trim();
  const httpOnly = /^HTTP\s+(\d{3})\b/i.exec(detail);
  if (httpOnly) {
    if (httpStatus == null) httpStatus = parseInt(httpOnly[1], 10);
    detail = detail.replace(/^HTTP\s+\d{3}\s*[:.\-–—]?\s*/i, '').trim();
  }
  let kind = (info.kind || '') as FetchFailureKind | '';
  if (!kind) kind = classifyFetchFailureKind(detail, httpStatus);
  const recovery = String(info.recoveryHint || PRODUCT_FETCH_RECOVERY_HINT).trim() || PRODUCT_FETCH_RECOVERY_HINT;
  const codeLabel = httpStatus != null
    ? 'HTTP ' + httpStatus
    : kind === 'panic'
      ? 'Backend panic'
      : kind === 'server'
        ? 'Backend error'
        : '';
  let statusCore: string;
  if (codeLabel && detail && detail !== codeLabel) statusCore = resource + ' failed: ' + codeLabel + ' — ' + detail;
  else if (codeLabel) statusCore = resource + ' failed: ' + codeLabel;
  else if (detail) statusCore = resource + ' failed: ' + detail;
  else if (kind === 'network') statusCore = resource + ' failed: network error';
  else statusCore = resource + ' failed';
  const status = statusCore + ' · ' + PRODUCT_FETCH_RECOVERY_SHORT;
  let bodyDetail: string;
  if (codeLabel && detail) bodyDetail = '<strong>' + escapeHtml(codeLabel) + '</strong> — ' + escapeHtml(detail);
  else if (codeLabel) bodyDetail = '<strong>' + escapeHtml(codeLabel) + '</strong>';
  else if (detail) bodyDetail = '<strong>Error</strong> — ' + escapeHtml(detail);
  else if (kind === 'network') bodyDetail = '<strong>Network error</strong> — request unreachable';
  else bodyDetail = '<strong>Error</strong> — request failed';
  const bodyHtml =
    '<div class="mvp-error" data-mvp-fetch-error="1" data-mvp-error-kind="' +
    escapeHtml(kind) +
    '">' +
    '<p class="mvp-error-title">' +
    escapeHtml(resource) +
    ' could not load</p>' +
    '<p class="mvp-error-body">' +
    bodyDetail +
    '</p>' +
    '<p class="mvp-error-hint">' +
    escapeHtml(recovery) +
    '</p></div>';
  return { title: resource, status, bodyHtml, httpStatus, kind: kind || 'unknown', recoveryHint: recovery };
}

export function productFetchFailureFromError(
  err: unknown,
  defaults?: { resource?: string; status?: number; kind?: string; recoveryHint?: string },
): FetchFailureView {
  const d = defaults || {};
  let message = '';
  let status = typeof d.status === 'number' ? d.status : null;
  let kind = d.kind || '';
  if (err && typeof err === 'object') {
    const e = err as { message?: unknown; httpStatus?: number; kind?: string };
    message = e.message != null ? String(e.message) : String(err);
    if (typeof e.httpStatus === 'number' && isFinite(e.httpStatus)) status = e.httpStatus;
    if (e.kind) kind = String(e.kind);
  } else if (err != null) {
    message = String(err);
  }
  if (status == null) {
    const m = /HTTP\s+(\d{3})\b/i.exec(message);
    if (m) status = parseInt(m[1], 10);
  }
  if (!kind) kind = classifyFetchFailureKind(message, status);
  return productFetchFailureView({
    resource: d.resource || 'Request',
    status: status != null ? status : undefined,
    message,
    kind: kind || undefined,
    recoveryHint: d.recoveryHint,
  });
}

export function emptyStateHtml(): string {
  return (
    '<div class="mvp-empty" data-mvp-empty="1">' +
    '<p class="mvp-empty-title">Project graph viz</p>' +
    '<p class="mvp-empty-body">No graph loaded. Open a Mermaid diagram from chat, ' +
    'use <strong>Load last</strong> for the pinned graph, or <strong>Paste</strong> a ' +
    'bullseye Mermaid export (```mermaid fence or raw source).</p>' +
    '<p class="mvp-empty-hint">Manual refresh only — live event refresh is deferred.</p>' +
    '</div>'
  );
}
