// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef, useEffect, useLayoutEffect, useState, useMemo } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { ConversationMeta } from '../conversation/useConversation';
import {
  clipClassName,
  expandTabChevron,
  isNearTranscriptEnd,
  lastMessageRowIndex,
  nextAutoExpanded,
  paintedClipHeight,
  shouldClip,
} from '../conversation/clip';
import { shouldRequestPage } from '../conversation/page';
import { displayRows, type DisplayKind, type StepItem } from '../conversation/display';
import { parseAssistantMarkdown } from '../conversation/markdown';
import { paintUserHTML, userBubbleClass, type TurnOrigin } from '../conversation/paint';
import { StreamingMarkdownBody } from '../conversation/StreamingMarkdownBody';
import { relTime } from '../relTime';
import { normalizeDensity, type Density } from '../density';
import { COMFORTABLE_ROW_GAP_PX, DEFAULT_ROW_GAP_PX, measureTranscriptRow } from '../transcript/rowLayout';
import { distanceFromEnd, followAfterScroll, pinWriteScrollTop } from '../transcript/followPin';
import {
  HYDRATE_OVERSCAN_MAX,
  measuredSuffixFromEnd,
  nextHydrateOverscan,
} from '../transcript/hydrateOverscan';
import { pixelFixtureRowTop } from '../visual/oldCockpitFixture';

export function AgentTranscript(props: {
  name: string;
  density?: Density;
  frames: unknown[];
  meta: ConversationMeta | null;
  ready?: boolean;
  followEpoch?: number;
  onPageOlder?: () => void;
  onLeaveLive?: () => void;
  onFollowChange?: (following: boolean) => void;
}) {
  const density = normalizeDensity(props.density);
  const parentRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);
  const pinningRef = useRef(false);
  const pinnedHydrate = useRef(false);
  const pagingRef = useRef(false);
  const wasReadyRef = useRef(false);
  const lastHeightRef = useRef(0);
  const hydrateSettled = useRef(false);
  const lastTotalRef = useRef(0);
  const lastCountRef = useRef(0);
  const pageStartRef = useRef(props.meta?.start);
  const [overscan, setOverscan] = useState(HYDRATE_OVERSCAN_MAX);
  const setFollow = (next: boolean) => {
    if (followRef.current === next) return;
    followRef.current = next;
    props.onFollowChange?.(next);
  };
  const rows = useMemo(() => displayRows(props.frames), [props.frames]);
  const count = rows.length;
  const latestMsg = useMemo(() => lastMessageRowIndex(rows.map((r) => r.kind)), [rows]);
  const estimate = density === 'compact' ? 48 : 72;
  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimate,
    overscan,
    gap: density === 'comfortable' ? COMFORTABLE_ROW_GAP_PX : DEFAULT_ROW_GAP_PX,
    measureElement: (el) => measureTranscriptRow(el),
  });

  useEffect(() => {
    pinnedHydrate.current = false;
    followRef.current = true;
    pinningRef.current = false;
    pagingRef.current = false;
    wasReadyRef.current = false;
    hydrateSettled.current = false;
    lastHeightRef.current = 0;
    lastTotalRef.current = 0;
    lastCountRef.current = 0;
    setOverscan(HYDRATE_OVERSCAN_MAX);
  }, [props.name]);

  useLayoutEffect(() => {
    if (props.ready && !wasReadyRef.current) followRef.current = true;
    wasReadyRef.current = !!props.ready;
  }, [props.ready]);

  const totalSize = virtualizer.getTotalSize();
  useLayoutEffect(() => {
    const el = parentRef.current;
    if (!el || !props.ready || count === 0) return;
    if (!followRef.current) return;
    pinningRef.current = true;
    virtualizer.scrollToIndex(count - 1, { align: 'end' });
    const pin = () => {
      if (!followRef.current) {
        pinningRef.current = false;
        return;
      }
      el.scrollTop = pinWriteScrollTop(el.scrollHeight);
      lastHeightRef.current = el.scrollHeight;
    };
    pin();
    const id = requestAnimationFrame(() => {
      pin();
      if (hydrateSettled.current) pinningRef.current = false;
    });
    pinnedHydrate.current = true;
    return () => {
      cancelAnimationFrame(id);
      pinningRef.current = false;
    };
  }, [props.ready, count, totalSize, props.name, virtualizer]);

  useLayoutEffect(() => {
    if (!props.ready || count === 0) return;
    if (count > lastCountRef.current && lastCountRef.current > 0) {
      hydrateSettled.current = false;
    }
    lastCountRef.current = count;
    if (totalSize !== lastTotalRef.current) {
      if (hydrateSettled.current && totalSize < lastTotalRef.current) {
        hydrateSettled.current = false;
      }
      lastTotalRef.current = totalSize;
    }
    if (hydrateSettled.current) return;
    const el = parentRef.current;
    const measured = new Map<number, number>();
    virtualizer.itemSizeCache.forEach((size, key) => {
      const idx = typeof key === 'number' ? key : Number(key);
      if (Number.isFinite(idx)) measured.set(idx, size);
    });
    const suffix = measuredSuffixFromEnd(measured, count);
    const next = nextHydrateOverscan({
      clientHeight: el?.clientHeight || 0,
      count,
      current: overscan,
      measuredFromEndPx: suffix.px,
      estimate,
      suffixComplete: suffix.complete,
    });
    if (next.settled) {
      hydrateSettled.current = true;
      if (overscan !== next.overscan) setOverscan(next.overscan);
      return;
    }
    if (next.overscan !== overscan) setOverscan(next.overscan);
  }, [props.ready, count, totalSize, overscan, estimate, virtualizer]);

  useEffect(() => {
    pageStartRef.current = props.meta?.start;
    pagingRef.current = false;
  }, [props.meta?.start, props.meta?.older]);

  useEffect(() => {
    const el = parentRef.current;
    if (!el || !props.onPageOlder) return;
    const requestOlder = () => {
      if (
        shouldRequestPage({
          scrollTop: el.scrollTop,
          older: props.meta?.older,
          inFlight: pagingRef.current,
          following: props.meta?.following !== false && followRef.current,
          scrollHeight: el.scrollHeight,
          clientHeight: el.clientHeight,
        })
      ) {
        pagingRef.current = true;
        props.onPageOlder?.();
      }
    };
    const onScroll = () => {
      const fromBottom = distanceFromEnd(el.scrollTop, el.scrollHeight, el.clientHeight);
      const next = followAfterScroll({
        fromBottom,
        pinning: pinningRef.current,
        wasFollowing: followRef.current,
        prevHeight: lastHeightRef.current,
        scrollHeight: el.scrollHeight,
      });
      lastHeightRef.current = next.height;
      setFollow(next.follow);
      requestOlder();
    };
    el.addEventListener('scroll', onScroll);
    const leave = () => {
      followRef.current = false;
      // PageUp must not freeze the mux window. leaveLive re-subscribes
      // the same range with a second HaloProse and prepends older rows,
      // so one PageDown cannot return to the live end (🎯T494.1.3).
    };
    el.addEventListener('jevons-leave-track', leave);
    el.addEventListener('jevons-page-older', requestOlder);
    return () => {
      el.removeEventListener('scroll', onScroll);
      el.removeEventListener('jevons-leave-track', leave);
      el.removeEventListener('jevons-page-older', requestOlder);
    };
  }, [props.meta?.older, props.meta?.start, props.onPageOlder, props.onLeaveLive]);

  const scroller = parentRef.current;
  const scrollTop = scroller?.scrollTop ?? 0;
  const clientHeight = scroller?.clientHeight ?? 0;
  const scrollHeight = scroller?.scrollHeight ?? totalSize;
  const nearEnd = isNearTranscriptEnd(scrollTop, scrollHeight, clientHeight);
  const historyReplayActive = !props.ready || !hydrateSettled.current;
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
              sealed={row.sealed === true}
              start={pixelFixtureRowTop(item.start, item.index, density)}
              measureRef={virtualizer.measureElement}
              isLatest={item.index === latestMsg}
              nearEnd={nearEnd}
              historyReplayActive={historyReplayActive}
              scrollTop={scrollTop}
              clientHeight={clientHeight}
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
  sealed?: boolean;
  origin?: TurnOrigin;
  start: number;
  measureRef?: (el: Element | null) => void;
}) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const [fullH, setFullH] = useState(0);
  const [expanded, setExpanded] = useState(false);
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const h = el.scrollHeight;
    setFullH((prev) => (prev === h ? prev : h));
  }, [props.text]);
  const tall = props.kind !== 'steps' && shouldClip(fullH);
  const pos = {
    position: 'absolute' as const,
    top: props.start,
  };
  if (props.kind === 'diagnostic') {
    return (
      <div
        data-index={props.index}
        data-kind="diagnostic"
        ref={props.measureRef}
        className="send-diag"
        style={{ ...pos, left: 0, right: 'auto' }}
      >
        {props.text}
      </div>
    );
  }
  if (props.kind === 'steps') {
    return (
      <div
        data-index={props.index}
        data-kind="steps"
        ref={props.measureRef}
        className="turn-marker"
        style={{ ...pos, left: 0, right: 0 }}
      >
        <span className="turn-label">
          {props.text}
          <div className="turn-tip">
            {(props.items || []).map((it, i) => (
              <div key={i} className={'turn-item ' + it.cls}>
                {it.text}
              </div>
            ))}
          </div>
        </span>
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
        props.sealed ? (
          <MarkdownBody text={props.text} bodyRef={bodyRef} />
        ) : (
          <StreamingMarkdownBody text={props.text} bodyRef={bodyRef} />
        )
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
          aria-expanded={expanded}
          aria-label={expanded ? 'Collapse' : 'Expand'}
          title={expanded ? 'Collapse' : 'Expand'}
          onClick={() => {
            setUserToggled(true);
            setAutoExpanded(false);
            setExpanded((v) => !v);
          }}
        >
          <span className="chev" aria-hidden="true">
            {expandTabChevron(expanded)}
          </span>
        </button>
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
