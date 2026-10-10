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
  const [lookup, setLookup] = useState<{ key: string; row: FrontierRow | null | 'ambiguous' } | null>(null);

  useLayoutEffect(() => {
    const root = props.containerRef.current;
    if (!root) return;
    const spots = [...root.querySelectorAll<HTMLElement>('.target-hotspot')];
    const onEnter = (e: Event) => {
      const el = e.currentTarget;
      if (el instanceof HTMLElement) setActive(el);
    };
    const onKey = (e: Event) => {
      if (!(e instanceof KeyboardEvent)) return;
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      onEnter(e);
    };
    for (const s of spots) {
      s.classList.add('has-instant-tip');
      s.addEventListener('pointerenter', onEnter);
      s.addEventListener('click', onEnter);
      s.addEventListener('keydown', onKey);
    }
    return () => {
      for (const s of spots) {
        s.removeEventListener('pointerenter', onEnter);
        s.removeEventListener('click', onEnter);
        s.removeEventListener('keydown', onKey);
      }
    };
  });

  useLayoutEffect(() => {
    if (!active || active.isConnected) return;
    const id = normalizeTargetID(active.getAttribute('data-target-id') || '');
    const repo = active.getAttribute('data-target-repo') || '';
    const root = props.containerRef.current;
    const next = id
      ? root?.querySelector<HTMLElement>('.target-hotspot[data-target-id="' + CSS.escape(id) + '"]' + (repo ? '[data-target-repo="' + CSS.escape(repo) + '"]' : ':not([data-target-repo])'))
      : null;
    setActive(next && next.isConnected ? next : null);
  });

  const tid = normalizeTargetID(active?.getAttribute('data-target-id') || '');
  const repo = active?.getAttribute('data-target-repo') || '';
  const found = repo ? null : findRowByTargetID(rows, tid) as FrontierRow | null;
  const lookupKey = repo + '/' + tid;
  const fetched = lookup?.key === lookupKey ? lookup.row : undefined;

  useEffect(() => {
    if (!tid || found) {
      setLookup(null);
      return;
    }
    let cancelled = false;
    setLookup(null);
    void fetchFrontierTarget(tid, undefined, repo || undefined)
      .then((row) => {
        if (!cancelled) setLookup({ key: lookupKey, row });
      })
      .catch((error: unknown) => {
        if (!cancelled) setLookup({ key: lookupKey, row: repo && String(error).includes('409') ? 'ambiguous' : null });
      });
    return () => {
      cancelled = true;
    };
  }, [tid, repo, found, lookupKey]);

  if (!active || !tid) return null;
  let md = '';
  let cardId = tid;
  let cardName = '';
  if (found) {
    md = hoverCardMarkdown(cacheRef.current, found, repo);
    cardId = found.id;
    cardName = found.name;
  } else if (fetched === 'ambiguous') {
    md = '**' + repo + '/🎯' + tid + '**\n\nAmbiguous repository alias; use a fully qualified repository name.';
  } else if (fetched) {
    md = hoverCardMarkdown(cacheRef.current, fetched, repo);
    cardId = fetched.id;
    cardName = fetched.name;
  } else if (fetched === null) {
    md = formatMissingTargetMarkdown(tid, repo);
  } else {
    md = '**' + (repo ? repo + '/' : '') + '🎯' + tid + '**';
  }
  if (repo && fetched !== null && fetched !== 'ambiguous') md = '**Repository:** ' + repo + '\n\n' + md;
  return (
    <InstantTip
      key={repo + '/' + tid}
      defaultOpen
      groupHosts={() => [active]}
      placement="toward-mid"
      cardClassName="target-card-tip"
      content={<TargetHoverCard markdown={md} id={cardId} name={cardName} foreign={!!repo} />}
    />
  );
}
