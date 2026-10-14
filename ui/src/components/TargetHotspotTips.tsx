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
  const [fetched, setFetched] = useState<{ key: string; row: FrontierRow | null } | undefined>(undefined);

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
      ? root?.querySelector<HTMLElement>('.target-hotspot[data-target-id="' + CSS.escape(id) + '"][data-target-repo="' + CSS.escape(repo) + '"]')
      : null;
    setActive(next && next.isConnected ? next : null);
  });

  const tid = normalizeTargetID(active?.getAttribute('data-target-id') || '');
  const repo = active?.getAttribute('data-target-repo') || '';
  // The frontier context is the current repository only; never use its T177
  // as the answer to an explicitly qualified claudia/T177.
  const found = repo ? null : findRowByTargetID(rows, tid) as FrontierRow | null;

  useEffect(() => {
    if (!tid || found) {
      setFetched(undefined);
      return;
    }
    let cancelled = false;
    setFetched(undefined);
    void fetchFrontierTarget(tid, undefined, repo)
      .then((row) => {
        if (!cancelled) setFetched({ key: repo + '/' + tid, row });
      })
      .catch(() => {
        if (!cancelled) setFetched({ key: repo + '/' + tid, row: null });
      });
    return () => {
      cancelled = true;
    };
  }, [tid, repo, found]);

  if (!active || !tid) return null;
  const resolved = fetched?.key === repo + '/' + tid ? fetched.row : undefined;
  let md = '';
  let cardId = tid;
  let cardName = '';
  if (found) {
    md = hoverCardMarkdown(cacheRef.current, found);
    cardId = found.id;
    cardName = found.name;
  } else if (resolved) {
    md = hoverCardMarkdown(cacheRef.current, resolved);
    cardId = resolved.id;
    cardName = resolved.name;
  } else if (resolved === null) {
    md = formatMissingTargetMarkdown(repo ? repo + '/' + tid : tid);
  } else {
    md = '**' + (repo ? repo + '/' : '') + '🎯' + tid + '**';
  }
  if (repo) md = '**Repo: ' + repo + '**\n\n' + md;
  return (
    <InstantTip
      key={repo + '/' + tid}
      defaultOpen
      onDismiss={() => setActive(null)}
      groupHosts={() => [active]}
      placement="toward-mid"
      cardClassName="target-card-tip"
      content={<TargetHoverCard markdown={md} id={cardId} name={cardName} />}
    />
  );
}
