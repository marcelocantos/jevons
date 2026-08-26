// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useLayoutEffect, useRef } from 'react';
import { TargetHotspotTips } from '../components/TargetHotspotTips';
import { ensureFenceNewlines, parseAssistantMarkdown } from './markdown';
import { renderMermaidIn } from './mermaidPaint';
import { createSession, type StreamSession } from './streamingMarkdown';

/** Live unsealed assistant: incremental smd, not marked-every-token. */
export function StreamingMarkdownBody(props: {
  text: string;
  bodyRef: React.RefObject<HTMLDivElement | null>;
}) {
  const sessionRef = useRef<StreamSession | null>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);

  const setRoot = (el: HTMLDivElement | null) => {
    rootRef.current = el;
    props.bodyRef.current = el;
  };

  useLayoutEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    if (!sessionRef.current) sessionRef.current = createSession(el);
    const session = sessionRef.current;
    if (!session) {
      el.innerHTML = parseAssistantMarkdown(props.text);
      return;
    }
    session.writeFull(props.text, ensureFenceNewlines);
    void renderMermaidIn(el);
  }, [props.text]);

  useEffect(() => {
    return () => {
      sessionRef.current?.destroy();
      sessionRef.current = null;
    };
  }, []);

  return (
    <>
      <div className="msg-body" ref={setRoot} />
      <TargetHotspotTips containerRef={rootRef} html={props.text} />
    </>
  );
}
