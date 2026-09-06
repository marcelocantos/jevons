// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useRef, useState } from 'react';
import { copyImageStatus, copyMermaidImage, copyMermaidSource, svgMarkupFrom } from '../conversation/mermaidClipboard';
import { renderMermaidSource } from '../conversation/mermaidPaint';
import {
  PACK_BLOCK_CHROME_H,
  applyPackPlacement,
  applySvgScaleToFill,
  emptyStateHtml,
  parseSvgNaturalSize,
  planFrontierGraphFit,
  planSingleGraphScaleToFill,
  productFetchFailureFromError,
  stripMermaidFence,
  type FetchFailureView,
} from '../conversation/graphFit';
import {
  normalizeGraphPayload,
  resolveFrontierGraphOpenPlan,
  type GraphDiagram,
} from '../frontier/graphPack';

/** Vanilla #mermaid-viz-panel (🎯T83 / T185 / T294). Graph opens diagrams[], never the joined pack source. */

type PackBlock = GraphDiagram & { svg?: string; fallback?: string };

export function MermaidVizPanel(props: {
  open: boolean;
  onClose: () => void;
  graphNonce: number;
  fetchGraph?: typeof fetch;
  renderSource?: (src: string) => Promise<string>;
}) {
  const [status, setStatus] = useState('');
  const [title, setTitle] = useState('Unachieved graph');
  const [src, setSrc] = useState('');
  const [mode, setMode] = useState<'loading' | 'error' | 'empty' | 'pack' | 'single'>('empty');
  const [errorHtml, setErrorHtml] = useState('');
  const [emptyHtml, setEmptyHtml] = useState('');
  const [singleSvg, setSingleSvg] = useState('');
  const [blocks, setBlocks] = useState<PackBlock[]>([]);
  const bodyRef = useRef<HTMLDivElement>(null);
  const fetchGraph = props.fetchGraph || fetch;
  const renderSource = props.renderSource || renderMermaidSource;

  const showFailure = useCallback((view: FetchFailureView) => {
    setMode('error');
    setTitle(view.title);
    setStatus(view.status);
    setErrorHtml(view.bodyHtml);
    setSrc('');
    setBlocks([]);
    setSingleSvg('');
  }, []);

  const loadGraph = useCallback(async () => {
    setMode('loading');
    setStatus('Loading unachieved dependency graph…');
    setErrorHtml('');
    setEmptyHtml('');
    setBlocks([]);
    setSingleSvg('');
    try {
      const r = await fetchGraph('/api/frontier/graph');
      const text = await r.text();
      let payload: unknown = text;
      if (text.trim().startsWith('{')) {
        payload = JSON.parse(text) as unknown;
      } else {
        payload = { available: !!text.trim(), mermaid: text };
      }
      if (!r.ok) {
        const err = Object.assign(new Error('HTTP ' + r.status), { httpStatus: r.status });
        showFailure(productFetchFailureFromError(err, { resource: 'Unachieved graph', status: r.status }));
        return;
      }
      const model = normalizeGraphPayload(payload);
      const payloadErr = String(model.error || '').trim();
      const openPlan = resolveFrontierGraphOpenPlan(model);
      if (!openPlan || openPlan.mode === 'empty') {
        if (payloadErr) {
          showFailure(productFetchFailureFromError({ message: payloadErr }, { resource: 'Unachieved graph' }));
          return;
        }
        setMode('empty');
        setTitle('Unachieved graph');
        setStatus((openPlan && openPlan.statusNote) || 'No unachieved graph available');
        setEmptyHtml(emptyStateHtml());
        setSrc('');
        return;
      }
      const nBlocks = openPlan.diagramCount || 1;
      const meta =
        model.nodeCount || model.edgeCount
          ? ' · ' +
            (model.nodeCount || 0) +
            ' nodes · ' +
            (model.edgeCount || 0) +
            ' edges · ' +
            nBlocks +
            ' diagram' +
            (nBlocks === 1 ? '' : 's')
          : '';
      const note = openPlan.statusNote ? ' · ' + openPlan.statusNote : '';
      const pinSrc = model.mermaid || openPlan.mermaid;
      setSrc(pinSrc);
      setTitle('Unachieved graph');
      if (openPlan.mode === 'pack') {
        const list = openPlan.diagrams || [];
        const rendered: PackBlock[] = [];
        for (let i = 0; i < list.length; i++) {
          const d = list[i];
          const source = stripMermaidFence(d.mermaid);
          const block: PackBlock = { ...d, id: d.id || 'd' + i };
          if (!source) {
            block.fallback = '(empty)';
          } else {
            try {
              block.svg = await renderSource(source);
            } catch (err) {
              block.fallback = source;
              block.title = (d.title || 'Diagram ' + (i + 1)) + ' (render failed)';
              void err;
            }
          }
          rendered.push(block);
        }
        setBlocks(rendered);
        setMode('pack');
        setStatus('Unachieved dependency graph' + meta + note);
        return;
      }
      const source = stripMermaidFence(openPlan.mermaid);
      try {
        const svg = await renderSource(source);
        setSingleSvg(svg);
        setMode('single');
        setStatus('Unachieved dependency graph' + meta + note);
      } catch (err) {
        showFailure(productFetchFailureFromError(err, { resource: 'Unachieved graph' }));
      }
    } catch (err) {
      showFailure(productFetchFailureFromError(err, { resource: 'Unachieved graph' }));
    }
  }, [fetchGraph, renderSource, showFailure]);

  const onCopySource = useCallback(async () => {
    try {
      await copyMermaidSource(src);
      setStatus('Source copied');
    } catch (err) {
      setStatus('Copy failed: ' + String(err instanceof Error ? err.message : err));
    }
  }, [src]);
  const onCopyImage = useCallback(async () => {
    try {
      const r = await copyMermaidImage(src, svgMarkupFrom(bodyRef.current));
      setStatus(copyImageStatus(r.mode));
    } catch (err) {
      setStatus('Copy failed: ' + String(err instanceof Error ? err.message : err));
    }
  }, [src]);

  useEffect(() => {
    if (!props.open || !props.graphNonce) return;
    void loadGraph();
  }, [props.open, props.graphNonce, loadGraph]);

  useEffect(() => {
    if (!props.open) return;
    const body = bodyRef.current;
    if (!body) return;
    const paneW = body.clientWidth || 0;
    const paneH = body.clientHeight || 0;
    if (!paneW || !paneH) return;
    if (mode === 'pack') {
      const nodes = Array.from(body.querySelectorAll<HTMLElement>('.mvp-pack-block'));
      const boxes = [];
      const items: Array<{ block: HTMLElement; svg: Element | null; naturalW: number; naturalH: number }> = [];
      let anyTitle = false;
      for (let i = 0; i < nodes.length; i++) {
        const block = nodes[i];
        const svg = block.querySelector('svg');
        if (!svg) continue;
        const nat = parseSvgNaturalSize(svg);
        let w = nat.w;
        let h = nat.h;
        if (!w || !h) {
          const r = svg.getBoundingClientRect();
          if (r.width > 0 && r.height > 0) {
            w = r.width;
            h = r.height;
          }
        }
        if (!w || !h) continue;
        if (block.querySelector('.mvp-pack-title')) anyTitle = true;
        boxes.push({ w, h, id: block.getAttribute('data-diagram-id') || 'd' + i });
        items.push({ block, svg, naturalW: w, naturalH: h });
      }
      if (!boxes.length) return;
      const chromeH = anyTitle ? PACK_BLOCK_CHROME_H : 20;
      const plan = planFrontierGraphFit({ boxes, paneW, paneH, padding: 0, gap: 12, chromeH });
      if (plan.mode !== 'pack-scale-to-fill' && plan.mode !== 'reflow-readable') return;
      body.classList.add('mvp-pack-scaled');
      body.style.position = 'relative';
      body.style.minHeight = '';
      body.style.minWidth = '';
      body.setAttribute('data-mvp-fit-mode', plan.mode);
      if (plan.labelPx > 0) body.setAttribute('data-mvp-label-px', String(plan.labelPx));
      const fits = plan.displayW <= paneW + 1 && plan.displayH <= paneH + 1;
      const offsetX = fits ? Math.max(0, (paneW - plan.displayW) / 2) : 0;
      const offsetY = fits ? Math.max(0, (paneH - plan.displayH) / 2) : 0;
      for (const pl of plan.placements) {
        const item = items[pl.i];
        if (!item) continue;
        applyPackPlacement(
          item.block,
          item.svg,
          {
            ...pl,
            naturalW: pl.naturalW != null ? pl.naturalW : item.naturalW,
            naturalH: pl.naturalH != null ? pl.naturalH : item.naturalH,
            displayX: pl.displayX + offsetX,
            displayY: pl.displayY + offsetY,
          },
          plan.scale,
        );
      }
      return;
    }
    if (mode === 'single') {
      const svg = body.querySelector('svg');
      if (!svg) return;
      const nat = parseSvgNaturalSize(svg);
      let svgW = nat.w;
      let svgH = nat.h;
      if (!svgW || !svgH) {
        const r = svg.getBoundingClientRect();
        if (r.width > 0 && r.height > 0) {
          svgW = r.width;
          svgH = r.height;
        }
      }
      const plan = planSingleGraphScaleToFill({ svgW, svgH, paneW, paneH, padding: 0, diagramCount: 1 });
      if (plan.mode !== 'scale-to-fill') return;
      body.classList.add('mvp-scale-fill');
      body.setAttribute('data-mvp-fit-mode', plan.floored ? 'floored-readable' : 'scale-to-fill');
      if (plan.labelPx > 0) body.setAttribute('data-mvp-label-px', String(plan.labelPx));
      applySvgScaleToFill(svg, plan);
    }
  }, [props.open, mode, blocks, singleSvg]);

  useEffect(() => {
    if (!props.open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') props.onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [props.open, props.onClose]);

  const bodyClass =
    'mvp-body' +
    (mode === 'pack' ? ' mvp-pack' : '') +
    (mode === 'single' ? ' mvp-scale-fill' : '');

  return (
    <div
      id="mermaid-viz-panel"
      role="dialog"
      aria-label="Project graph visualization"
      aria-modal="false"
      hidden={!props.open}
      className={props.open ? 'open mvp-large' : undefined}
    >
      <div className="mvp-header">
        <span className="mvp-title" id="mvp-title">
          {title}
        </span>
        <button type="button" id="mvp-copy-source" className="mermaid-action" data-action="copy-source" title="Copy Mermaid source" disabled={!src} onClick={onCopySource}>
          Copy source
        </button>
        <button type="button" id="mvp-copy-image" className="mermaid-action" data-action="copy-image" title="Copy diagram image (PNG + source when supported)" disabled={!src} onClick={onCopyImage}>
          Copy image
        </button>
        <button type="button" id="mvp-close" title="Close panel" onClick={props.onClose}>
          Close
        </button>
      </div>
      <div className={bodyClass} id="mvp-body" ref={bodyRef}>
        {mode === 'loading' ? <p className="mvp-empty-body" style={{ padding: 12 }}>Loading…</p> : null}
        {mode === 'error' ? <div dangerouslySetInnerHTML={{ __html: errorHtml }} /> : null}
        {mode === 'empty' ? <div dangerouslySetInnerHTML={{ __html: emptyHtml }} /> : null}
        {mode === 'single' && singleSvg ? <div dangerouslySetInnerHTML={{ __html: singleSvg }} /> : null}
        {mode === 'pack'
          ? blocks.map((d, i) => (
              <div key={d.id || 'd' + i} className="mvp-pack-block" data-kind={d.kind || 'component'} data-diagram-id={d.id || 'd' + i}>
                <p className="mvp-pack-title">{d.title || 'Diagram ' + (i + 1)}</p>
                <div className="mvp-pack-host">
                  {d.svg ? (
                    <div dangerouslySetInnerHTML={{ __html: d.svg }} />
                  ) : (
                    <pre className="mvp-source-fallback" style={{ textAlign: 'left', fontSize: 11, whiteSpace: 'pre-wrap', margin: 0 }}>
                      {d.fallback || ''}
                    </pre>
                  )}
                </div>
              </div>
            ))
          : null}
      </div>
      <div className="mvp-status" id="mvp-status">
        {status}
      </div>
    </div>
  );
}
