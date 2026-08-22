// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef, useEffect, useState, useMemo } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { marked } from 'marked';
import type { ConversationMeta } from '../conversation/useConversation';
import { clipClassName, shouldClip } from '../conversation/clip';
import { shouldRequestPage } from '../conversation/page';
import { displayRows, type DisplayKind } from '../conversation/display';

export function AgentTranscript(props: {
  name: string;
  frames: unknown[];
  meta: ConversationMeta | null;
  ready?: boolean;
  onPageOlder?: () => void;
}) {
  const parentRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  const pinnedHydrate = useRef(false);
  const pagingRef = useRef(false);
  const pageStartRef = useRef(props.meta?.start);
  const rows = useMemo(() => displayRows(props.frames), [props.frames]);
  const count = rows.length;
  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 72,
    overscan: 12,
  });

  useEffect(() => {
    pinnedHydrate.current = false;
    followRef.current = true;
    pagingRef.current = false;
  }, [props.name]);

  useEffect(() => {
    if (!props.ready || count === 0) return;
    if (!pinnedHydrate.current) {
      pinnedHydrate.current = true;
      const id = requestAnimationFrame(() => {
        virtualizer.scrollToIndex(count - 1, { align: 'end' });
      });
      return () => cancelAnimationFrame(id);
    }
    if (followRef.current) {
      virtualizer.scrollToIndex(count - 1, { align: 'end' });
    }
  }, [props.ready, count, virtualizer]);

  useEffect(() => {
    if (pageStartRef.current !== props.meta?.start) {
      pageStartRef.current = props.meta?.start;
      pagingRef.current = false;
    }
  }, [props.meta?.start]);

  useEffect(() => {
    const el = parentRef.current;
    if (!el || !props.onPageOlder) return;
    const onScroll = () => {
      const fromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      followRef.current = fromBottom < 80;
      if (
        shouldRequestPage({
          scrollTop: el.scrollTop,
          older: props.meta?.older,
          inFlight: pagingRef.current,
        })
      ) {
        pagingRef.current = true;
        props.onPageOlder?.();
      }
    };
    el.addEventListener('scroll', onScroll);
    return () => el.removeEventListener('scroll', onScroll);
  }, [props.meta?.older, props.meta?.start, props.onPageOlder]);

  return (
    <div className="agent-transcript" ref={parentRef}>
      <div
        style={{
          height: virtualizer.getTotalSize(),
          width: '100%',
          position: 'relative',
        }}
      >
        {virtualizer.getVirtualItems().map((item) => {
          const row = rows[item.index];
          return (
            <ClippedBubble
              key={item.key}
              index={item.index}
              kind={row.kind}
              text={row.text}
              start={item.start}
              measureRef={virtualizer.measureElement}
            />
          );
        })}
      </div>
    </div>
  );
}

function ClippedBubble(props: {
  index: number;
  kind: DisplayKind;
  text: string;
  start: number;
  measureRef: (el: Element | null) => void;
}) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const [fullH, setFullH] = useState(0);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    setFullH(el.scrollHeight);
  }, [props.text]);
  const tall = shouldClip(fullH);
  const base = `bubble bubble-${props.kind} msg`;
  const cls = expanded || props.kind === 'steps' ? base : clipClassName(base, fullH);
  return (
    <div
      data-index={props.index}
      ref={props.measureRef}
      className={cls}
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        width: '100%',
        transform: `translateY(${props.start}px)`,
      }}
    >
      <div className="bubble-role">{props.kind === 'steps' ? '' : props.kind}</div>
      {props.kind === 'assistant' ? (
        <div
          className="bubble-body msg-body md"
          ref={bodyRef}
          dangerouslySetInnerHTML={{ __html: marked.parse(props.text, { async: false }) as string }}
        />
      ) : (
        <div className="bubble-body msg-body" ref={bodyRef}>
          {props.text}
        </div>
      )}
      {tall ? (
        <button
          type="button"
          className="msg-expand-tab"
          aria-label={expanded ? 'collapse' : 'expand'}
          onClick={() => setExpanded((v) => !v)}
        />
      ) : null}
    </div>
  );
}
