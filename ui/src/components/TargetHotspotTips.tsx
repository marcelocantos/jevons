// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import {
  fetchFrontierTarget,
  formatMissingTargetMarkdown,
  hoverCardMarkdown,
  type FrontierRow,
  type HoverCardCache,
} from '../frontier/table';
import { findRowByTargetID, normalizeTargetID } from '../frontier/targetHotspot';
import { useFrontierRows } from '../frontier/rows';
import { InstantTip } from './InstantTip';
import { TargetHoverCard } from './TargetHoverCard';

/** One InstantTip per bubble, opened on hotspot enter (🎯T326 / T647). */
export function TargetHotspotTips(props: {
  containerRef: RefObject<HTMLElement | null>;
  html?: string;
}) {
  const rows = useFrontierRows();
  const cacheRef = useRef<HoverCardCache>({});
  const [active, setActive] = useState<HTMLElement | null>(null);
  const [fetched, setFetched] = useState<FrontierRow | null | undefined>(undefined);

  useLayoutEffect(() => {
    const root = props.containerRef.current;
    if (!root) return;
    const spots = [...root.querySelectorAll<HTMLElement>('.target-hotspot')];
    const onEnter = (e: Event) => {
      const el = e.currentTarget;
      if (el instanceof HTMLElement) setActive(el);
    };
    for (const s of spots) {
      s.classList.add('has-instant-tip');
      s.addEventListener('pointerenter', onEnter);
    }
    return () => {
      for (const s of spots) s.removeEventListener('pointerenter', onEnter);
    };
  });

  useLayoutEffect(() => {
    if (active && !active.isConnected) setActive(null);
  });

  const tid = normalizeTargetID(active?.getAttribute('data-target-id') || '');
  const found = findRowByTargetID(rows, tid) as FrontierRow | null;

  useEffect(() => {
    if (!tid || found) {
      setFetched(undefined);
      return;
    }
    let cancelled = false;
    setFetched(undefined);
    void fetchFrontierTarget(tid)
      .then((row) => {
        if (!cancelled) setFetched(row);
      })
      .catch(() => {
        if (!cancelled) setFetched(null);
      });
    return () => {
      cancelled = true;
    };
  }, [tid, found]);

  if (!active || !tid) return null;
  let md = '';
  let cardId = tid;
  let cardName = '';
  if (found) {
    md = hoverCardMarkdown(cacheRef.current, found);
    cardId = found.id;
    cardName = found.name;
  } else if (fetched) {
    md = hoverCardMarkdown(cacheRef.current, fetched);
    cardId = fetched.id;
    cardName = fetched.name;
  } else if (fetched === null) {
    md = formatMissingTargetMarkdown(tid);
  } else {
    md = '**🎯' + tid + '**';
  }
  return (
    <InstantTip
      key={tid}
      defaultOpen
      groupHosts={() => [active]}
      placement="right-of-host"
      cardClassName="target-card-tip"
      content={<TargetHoverCard markdown={md} id={cardId} name={cardName} />}
    />
  );
}
