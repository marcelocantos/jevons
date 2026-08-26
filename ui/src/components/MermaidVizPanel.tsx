// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';
import { renderMermaidIn } from '../conversation/mermaidPaint';

/** Vanilla #mermaid-viz-panel (🎯T83 / T185). Graph opens the unachieved ledger. */

export function MermaidVizPanel(props: { open: boolean; onClose: () => void; graphNonce: number }) {
  const [status, setStatus] = useState('');
  const [bodyHtml, setBodyHtml] = useState('');

  const loadGraph = useCallback(async () => {
    setStatus('Loading unachieved dependency graph…');
    setBodyHtml('<p class="mvp-empty-body" style="padding:12px">Loading…</p>');
    try {
      const r = await fetch('/api/frontier/graph');
      if (!r.ok) {
        setStatus('Frontier graph HTTP ' + r.status);
        setBodyHtml(
          '<div class="mvp-error"><p class="mvp-error-title">Graph failed</p>' +
            '<p class="mvp-error-body">GET /api/frontier/graph returned ' +
            r.status +
            '</p></div>',
        );
        return;
      }
      const text = await r.text();
      let src = text;
      if (text.trim().startsWith('{')) {
        const j = JSON.parse(text) as { mermaid?: string; source?: string };
        src = String(j.mermaid || j.source || '');
      }
      const fence = src.includes('```') ? src : '```mermaid\n' + src + '\n```';
      const marked = await import('../conversation/markdown');
      setBodyHtml(marked.parseAssistantMarkdown(fence));
      setStatus('Unachieved graph');
    } catch (err) {
      setStatus('Frontier graph failed');
      setBodyHtml(
        '<div class="mvp-error"><p class="mvp-error-title">Graph failed</p>' +
          '<p class="mvp-error-body">' +
          String(err instanceof Error ? err.message : err) +
          '</p></div>',
      );
    }
  }, []);

  useEffect(() => {
    if (!props.open || !props.graphNonce) return;
    void loadGraph();
  }, [props.open, props.graphNonce, loadGraph]);

  useEffect(() => {
    if (!props.open) return;
    const body = document.getElementById('mvp-body');
    void renderMermaidIn(body);
  }, [props.open, bodyHtml]);

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
          Unachieved graph
        </span>
        <button type="button" id="mvp-close" title="Close panel" onClick={props.onClose}>
          Close
        </button>
      </div>
      <div className="mvp-body" id="mvp-body" dangerouslySetInnerHTML={{ __html: bodyHtml }} />
      <div className="mvp-status" id="mvp-status">
        {status}
      </div>
    </div>
  );
}
