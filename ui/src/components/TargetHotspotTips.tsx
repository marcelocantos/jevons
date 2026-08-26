// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { hoverCardMarkdown, type FrontierRow, type HoverCardCache } from '../frontier/table';
import { findRowByTargetID, minimalRowForID } from '../frontier/targetHotspot';
import { useFrontierRows } from '../frontier/rows';
import { InstantTip } from './InstantTip';
import { TargetHoverCard } from './TargetHoverCard';

/** One InstantTip per bubble, opened on hotspot enter (🎯T326). Not one mermaid card per 🎯Tn. */
export function TargetHotspotTips(props: {
  containerRef: RefObject<HTMLElement | null>;
  html?: string;
}) {
  const rows = useFrontierRows();
  const cacheRef = useRef<HoverCardCache>({});
  const [active, setActive] = useState<HTMLElement | null>(null);

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

  if (!active) return null;
  const tid = active.getAttribute('data-target-id') || '';
  const found = findRowByTargetID(rows, tid);
  const row = (found || minimalRowForID(tid)) as FrontierRow | null;
  if (!row) return null;
  const md = hoverCardMarkdown(cacheRef.current, row);
  return (
    <InstantTip
      key={tid}
      defaultOpen
      groupHosts={() => [active]}
      placement="right-of-host"
      cardClassName="target-card-tip"
      content={<TargetHoverCard markdown={md} id={row.id} name={row.name} />}
    />
  );
}
