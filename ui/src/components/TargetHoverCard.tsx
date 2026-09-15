// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useRef } from 'react';
import { parseAssistantMarkdown } from '../conversation/markdown';
import { renderMermaidIn } from '../conversation/mermaidPaint';

/**
 * 🎯T660: the painted card, keyed by its raw HTML. renderMermaidIn is async
 * (script load, then mermaid.render); between a (re)mount that sets the raw
 * fence and the paint that replaces it, the card shows mermaid source. Under
 * transcript activity the hotspot host is re-painted and the tip re-pinned
 * often enough that the raw fence kept winning. Once a card has been painted
 * its finished HTML is remembered, so every later mount of the same card
 * starts from the diagram and never from the fence.
 */
const paintedByHtml = new Map<string, string>();

/** Test / diagnostics seam: forget every painted card. */
export function resetPaintedHoverCards(): void {
  paintedByHtml.clear();
}

/** Inner body of a frontier InstantTip card (🎯T181 / T184): HTML + mermaid SVG. */
export function TargetHoverCard(props: { markdown: string; id?: string; name?: string }) {
  const html = parseAssistantMarkdown(props.markdown);
  const painted = paintedByHtml.get(html);
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (painted) {
      // Mounted from the remembered diagram: nothing left to paint. A
      // stale fence can only be here if React reset the node; repaint then.
      if (!el.querySelector('code.language-mermaid')) return;
    }
    let cancelled = false;
    void renderMermaidIn(el).then(() => {
      if (cancelled || !el.isConnected) return;
      if (el.querySelector('.mermaid-diagram') && !el.querySelector('code.language-mermaid')) {
        paintedByHtml.set(html, el.innerHTML);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [html, painted]);
  return <div className="target-hover-md" ref={ref} dangerouslySetInnerHTML={{ __html: painted ?? html }} />;
}
