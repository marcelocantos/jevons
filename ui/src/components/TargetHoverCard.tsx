// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useRef } from 'react';
import { parseAssistantMarkdown } from '../conversation/markdown';
import { renderMermaidIn } from '../conversation/mermaidPaint';

/** Inner body of a frontier InstantTip card (🎯T181 / T184): HTML + mermaid SVG. */
export function TargetHoverCard(props: { markdown: string; id?: string; name?: string }) {
  const html = parseAssistantMarkdown(props.markdown);
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    void renderMermaidIn(ref.current);
  }, [html]);
  return <div className="target-hover-md" ref={ref} dangerouslySetInnerHTML={{ __html: html }} />;
}
