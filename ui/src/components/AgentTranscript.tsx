// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef, useEffect } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { ConversationMeta } from '../conversation/useConversation';

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
    const el = parentRef.current;
    if (!el || !props.onPageOlder || !props.meta?.older) return;
    const onScroll = () => {
      const fromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      followRef.current = fromBottom < 80;
      if (el.scrollTop < 48) props.onPageOlder?.();
    };
    el.addEventListener('scroll', onScroll);
    return () => el.removeEventListener('scroll', onScroll);
  }, [props.meta?.older, props.onPageOlder]);

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
            <div
              key={item.key}
              data-index={item.index}
              ref={virtualizer.measureElement}
              className={`bubble bubble-${role}`}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${item.start}px)`,
              }}
            >
              <div className="bubble-role">{role}</div>
              <div className="bubble-body">{frameText(frame)}</div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
