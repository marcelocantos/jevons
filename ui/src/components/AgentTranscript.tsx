// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef, useEffect, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { ConversationMeta } from '../conversation/useConversation';
import { clipClassName, shouldClip } from '../conversation/clip';
import { shouldRequestPage } from '../conversation/page';

function frameText(frame: unknown): string {
  if (!frame || typeof frame !== 'object') return '';
  const f = frame as Record<string, unknown>;
  const msg = f.message as Record<string, unknown> | undefined;
  const content = msg?.content ?? f.text ?? f.content;
  if (typeof content === 'string') return content;
  if (Array.isArray(content)) {
    return content
      .map((b) => {
        if (b && typeof b === 'object' && 'text' in (b as object)) {
          return String((b as { text?: string }).text || '');
        }
        return '';
      })
      .join('');
  }
  return JSON.stringify(frame);
}

function frameRole(frame: unknown): string {
  if (!frame || typeof frame !== 'object') return 'unknown';
  const f = frame as Record<string, unknown>;
  if (typeof f.type === 'string') return f.type;
  const msg = f.message as Record<string, unknown> | undefined;
  if (typeof msg?.role === 'string') return msg.role;
  return 'unknown';
}

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
  const count = props.frames.length;
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
          const frame = props.frames[item.index];
          const role = frameRole(frame);
          return (
            <ClippedBubble
              key={item.key}
              index={item.index}
              role={role}
              text={frameText(frame)}
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
  role: string;
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
  const cls = expanded ? `bubble bubble-${props.role} msg` : clipClassName(`bubble bubble-${props.role} msg`, fullH);
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
      <div className="bubble-role">{props.role}</div>
      <div className="bubble-body msg-body" ref={bodyRef}>
        {props.text}
      </div>
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
