// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { type ReactNode, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import {
  HIDE_GRACE_MS,
  computeHitParts,
  hitRectIsDegenerate,
  placeCardRect,
  shouldDismissPointerSample,
  stickCardRect,
  type HitParts,
} from './instantTipHit';

export { HIDE_GRACE_MS };

/** Product-wide singleton (🎯T203): one InstantTip panel visible. */
let openCloser: (() => void) | null = null;

export type InstantTipPlacement = 'left-of-host' | 'right-of-host' | 'below-host';

/**
 * 🎯T271: hit region = card ∪ hosts ∪ horizontal corridor. HIDE_GRACE_MS=0.
 * Leave the region (including up/down off the row band) dismisses immediately.
 * Flicker → fix geometry, never a timeout. T186/T187/T231 are this path.
 * 🎯T643: persistHosts join the hit region without opening on enter.
 */
export function InstantTip(props: {
  id?: string;
  content: ReactNode;
  children?: ReactNode;
  className?: string;
  cardClassName?: string;
  /** Extra hosts that both open and persist (🎯T231). */
  groupHosts?: () => Array<Element | null | undefined>;
  /** Hit-region only — pointerenter does not open (🎯T643 row band). */
  persistHosts?: () => Array<Element | null | undefined>;
  placement?: InstantTipPlacement;
  /** T186: clamp card right edge left of these nodes (frontier table). */
  clampSelectors?: readonly string[];
  /** Mount already open (delegated T326 attach). */
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(!!props.defaultOpen);
  const closeRef = useRef(() => setOpen(false));
  closeRef.current = () => setOpen(false);
  const reactId = useId();
  const hostAttr = props.id || reactId;

  useEffect(() => {
    if (!open) return;
    const close = () => closeRef.current();
    if (openCloser && openCloser !== close) openCloser();
    openCloser = close;
    return () => {
      if (openCloser === close) openCloser = null;
    };
  }, [open]);

  const wrapRef = useRef<HTMLSpanElement>(null);
  const cardRef = useRef<HTMLElement>(null);
  const groupHostsRef = useRef(props.groupHosts);
  groupHostsRef.current = props.groupHosts;
  const persistHostsRef = useRef(props.persistHosts);
  persistHostsRef.current = props.persistHosts;
  const stickyRef = useRef<{ left: number; top: number } | null>(null);
  const lastXYRef = useRef<{ x: number; y: number } | null>(null);
  const lastPartsRef = useRef<HitParts | null>(null);

  const collectOpenHosts = (): Element[] => {
    const out: Element[] = [];
    const wrap = wrapRef.current;
    if (wrap) out.push(wrap);
    for (const h of groupHostsRef.current?.() || []) {
      if (h && out.indexOf(h) < 0) out.push(h);
    }
    return out;
  };

  const collectHitHosts = (): Element[] => {
    const out = collectOpenHosts();
    for (const h of persistHostsRef.current?.() || []) {
      if (h && out.indexOf(h) < 0) out.push(h);
    }
    return out;
  };

  useLayoutEffect(() => {
    const hosts = collectOpenHosts();
    const enter = () => setOpen(true);
    for (const h of hosts) {
      h.classList.add('has-instant-tip');
      if (!h.hasAttribute('data-instant-tip-host')) {
        h.setAttribute('data-instant-tip-host', hostAttr);
      }
      h.addEventListener('pointerenter', enter);
    }
    return () => {
      for (const h of hosts) {
        h.removeEventListener('pointerenter', enter);
      }
    };
  });

  const applyPlace = () => {
    const hosts = collectOpenHosts();
    const host = hosts[0];
    const card = cardRef.current;
    if (!host || !card) return;
    let clampRight: number | null = null;
    if (props.placement === 'left-of-host' && props.clampSelectors && typeof document !== 'undefined') {
      for (const sel of props.clampSelectors) {
        const el = document.querySelector(sel);
        if (el) {
          clampRight = el.getBoundingClientRect().left - 8;
          break;
        }
      }
    }
    const viewW = typeof window !== 'undefined' ? window.innerWidth : 0;
    const viewH = typeof window !== 'undefined' ? window.innerHeight : 0;
    const tipW = card.offsetWidth || 360;
    const tipH = card.offsetHeight || 80;
    const pos = stickyRef.current
      ? { ...stickCardRect({ ...stickyRef.current, tipW, tipH, viewW, viewH }), maxWidth: undefined }
      : placeCardRect({
          placement: props.placement || 'left-of-host',
          host: host.getBoundingClientRect(),
          tipW,
          tipH,
          viewW,
          viewH,
          clampRight,
        });
    stickyRef.current = { left: pos.left, top: pos.top };
    const left = pos.left + 'px';
    const top = pos.top + 'px';
    if (card.style.left !== left) card.style.left = left;
    if (card.style.top !== top) card.style.top = top;
    if (pos.maxWidth != null) {
      const mw = pos.maxWidth + 'px';
      if (card.style.maxWidth !== mw) card.style.maxWidth = mw;
    }
  };

  useLayoutEffect(() => {
    if (!open) {
      stickyRef.current = null;
      lastXYRef.current = null;
      lastPartsRef.current = null;
      return;
    }
    applyPlace();
  }, [open, props.content, props.placement, props.clampSelectors]);

  useEffect(() => {
    if (!open) return;
    const card = cardRef.current;
    if (!card || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => applyPlace());
    ro.observe(card);
    return () => ro.disconnect();
  }, [open, props.content, props.placement, props.clampSelectors]);

  useEffect(() => {
    if (!open) return;
    const sample = (x: number, y: number) => {
      const card = cardRef.current;
      const hosts = collectHitHosts();
      if (!card || !hosts.length) return;
      const parts = computeHitParts({
        cardRect: card.getBoundingClientRect(),
        hostRects: hosts.map((h) => h.getBoundingClientRect()),
      });
      const lastXY = lastXYRef.current;
      const lastParts = lastPartsRef.current;
      if (!hitRectIsDegenerate(parts.card)) lastPartsRef.current = parts;
      lastXYRef.current = { x, y };
      if (shouldDismissPointerSample({ x, y, lastXY, parts, lastParts })) setOpen(false);
    };
    const onMove = (e: PointerEvent) => sample(e.clientX, e.clientY);
    document.addEventListener('pointermove', onMove);
    return () => document.removeEventListener('pointermove', onMove);
  }, [open]);

  const card = (
    <aside
      ref={cardRef}
      className={
        'instant-tip instant-tip-card' +
        (open ? ' instant-tip-show' : '') +
        (props.cardClassName ? ' ' + props.cardClassName : '')
      }
      data-instant-tip={hostAttr}
      onPointerEnter={() => setOpen(true)}
    >
      {open ? props.content : null}
    </aside>
  );

  if (props.children == null) return card;

  return (
    <span
      ref={wrapRef}
      id={props.id}
      className={'has-instant-tip' + (props.className ? ' ' + props.className : '')}
      data-instant-tip-host={hostAttr}
      onPointerEnter={() => setOpen(true)}
    >
      {props.children}
      {card}
    </span>
  );
}

export function nativeTitleForbidden(el: Element | null): boolean {
  if (!el) return true;
  return !el.hasAttribute('title') || el.getAttribute('title') === '';
}

if (HIDE_GRACE_MS !== 0) {
  throw new Error('InstantTip product path forbids hide grace (🎯T271)');
}
