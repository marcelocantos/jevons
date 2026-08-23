// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef, useEffect, useLayoutEffect, useState, useMemo } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { ConversationMeta } from '../conversation/useConversation';
import { clipClassName, shouldClip } from '../conversation/clip';
import { shouldRequestPage } from '../conversation/page';
import { displayRows, type DisplayKind, type StepItem } from '../conversation/display';
import { parseAssistantMarkdown } from '../conversation/markdown';
import { renderUserTextWithImages } from '../composer/images';
import { relTime } from '../relTime';
import { normalizeDensity, type Density } from '../density';
import { COMFORTABLE_ROW_GAP_PX, DEFAULT_ROW_GAP_PX, measureTranscriptRow } from '../transcript/rowLayout';
import { pixelFixtureRowTop } from '../visual/oldCockpitFixture';

export function AgentTranscript(props: {
  name: string;
  density?: Density;
  frames: unknown[];
  meta: ConversationMeta | null;
  ready?: boolean;
  onPageOlder?: () => void;
}) {
  const density = normalizeDensity(props.density);
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
    estimateSize: () => (density === 'compact' ? 48 : 72),
    overscan: 12,
    gap: density === 'comfortable' ? COMFORTABLE_ROW_GAP_PX : DEFAULT_ROW_GAP_PX,
    measureElement: (el) => measureTranscriptRow(el),
  });

  useEffect(() => {
    pinnedHydrate.current = false;
    followRef.current = true;
    pagingRef.current = false;
  }, [props.name]);

  const totalSize = virtualizer.getTotalSize();
  useLayoutEffect(() => {
    const el = parentRef.current;
    if (!el || !props.ready || count === 0) return;
    if (!followRef.current) return;
    const pin = () => {
      if (!followRef.current) return;
      el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight);
    };
    pin();
    const id = requestAnimationFrame(pin);
    pinnedHydrate.current = true;
    return () => cancelAnimationFrame(id);
  }, [props.ready, count, totalSize, props.name]);

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
    const leave = () => {
      followRef.current = false;
    };
    el.addEventListener('jevons-leave-track', leave);
    return () => {
      el.removeEventListener('scroll', onScroll);
      el.removeEventListener('jevons-leave-track', leave);
    };
  }, [props.meta?.older, props.meta?.start, props.onPageOlder]);

  const bodyId = density === 'compact' ? 'agent-inspect-body' : 'messages';
  return (
    <div id={bodyId} ref={parentRef}>
      {density === 'comfortable' ? <div className="history-sentinel" /> : null}
      <div
        id={density === 'compact' ? undefined : 'messages-canvas'}
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
              items={row.items}
              when={row.when}
              start={pixelFixtureRowTop(item.start, item.index, density)}
              measureRef={virtualizer.measureElement}
            />
          );
        })}
      </div>
      {density === 'comfortable' ? (
        <div
          id="messages-end"
          aria-hidden="true"
          style={{
            height: 0,
            width: '100%',
            flex: '0 0 auto',
            overflowAnchor: 'none',
            pointerEvents: 'none',
          }}
        />
      ) : null}
    </div>
  );
}

function ClippedBubble(props: {
  index: number;
  kind: DisplayKind;
  text: string;
  items?: StepItem[];
  when?: number;
  start: number;
  measureRef?: (el: Element | null) => void;
}) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const [fullH, setFullH] = useState(0);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    setFullH(el.scrollHeight);
  }, [props.text]);
  const tall = props.kind !== 'steps' && shouldClip(fullH);
  const pos = {
    position: 'absolute' as const,
    top: props.start,
  };
  if (props.kind === 'steps') {
    return (
      <div
        data-index={props.index}
        data-kind="steps"
        ref={props.measureRef}
        className="turn-marker"
        style={{ ...pos, left: 0, right: 0 }}
      >
        <span>{props.text}</span>
        <div className="turn-tip">
          {(props.items || []).map((it, i) => (
            <div key={i} className={'turn-item ' + it.cls}>
              {it.text}
            </div>
          ))}
        </div>
      </div>
    );
  }
  const roleClass = props.kind === 'user' ? 'user' : 'jevons';
  const base = `msg ${roleClass}`;
  const cls = expanded ? base : clipClassName(base, fullH);
  return (
    <div
      data-index={props.index}
      data-kind={props.kind}
      ref={props.measureRef}
      className={cls}
      style={{
        ...pos,
        left: props.kind === 'user' ? 'auto' : 0,
        right: props.kind === 'user' ? 0 : 'auto',
      }}
    >
      {props.kind === 'assistant' ? (
        <MarkdownBody text={props.text} bodyRef={bodyRef} />
      ) : (
        <UserBody text={props.text} bodyRef={bodyRef} />
      )}
      {props.when != null ? (
        <div className="msg-time" data-ts={props.when}>
          {relTime(props.when)}
        </div>
      ) : null}
      {tall ? (
        <button
          type="button"
          tabIndex={-1}
          className="msg-expand-tab"
          aria-label={expanded ? 'collapse' : 'expand'}
          onClick={() => setExpanded((v) => !v)}
        />
      ) : null}
    </div>
  );
}

function MarkdownBody(props: { text: string; bodyRef: React.RefObject<HTMLDivElement | null> }) {
  const html = parseAssistantMarkdown(props.text);
  useEffect(() => {
    const el = props.bodyRef.current;
    if (!el) return;
    el.querySelectorAll('a').forEach((a) => a.setAttribute('tabindex', '-1'));
  }, [html, props.bodyRef]);
  return (
    <div className="msg-body" ref={props.bodyRef} dangerouslySetInnerHTML={{ __html: html }} />
  );
}

function UserBody(props: { text: string; bodyRef: React.RefObject<HTMLDivElement | null> }) {
  const html = renderUserTextWithImages(props.text);
  return (
    <div className="msg-body" ref={props.bodyRef} dangerouslySetInnerHTML={{ __html: html }} />
  );
}
