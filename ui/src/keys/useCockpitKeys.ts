// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect } from 'react';
import { planComposerTabCycle } from './composerTab';
import { pageScrollDelta } from './pageScroll';

function composerKind(el: Element | null): 'main' | 'sidebar' | 'other' {
  if (!el) return 'other';
  const box = el.closest('textarea[data-composer]');
  if (!box) return 'other';
  const kind = box.getAttribute('data-composer');
  if (kind === 'main' || kind === 'sidebar') return kind;
  return 'other';
}

function transcriptNear(el: Element | null): HTMLElement | null {
  if (el) {
    const wrap = el.closest('.agent-interaction');
    const pane = wrap?.querySelector('.agent-transcript');
    if (pane instanceof HTMLElement) return pane;
  }
  const main = document.querySelector('.cockpit-body > .agent-interaction .agent-transcript');
  return main instanceof HTMLElement ? main : null;
}

export function useCockpitKeys(opts: { sidebarComposerVisible: boolean }): void {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const active = composerKind(document.activeElement);
      const plan = planComposerTabCycle(e, {
        active,
        sidebarVisible: opts.sidebarComposerVisible,
      });
      if (plan.preventDefault && plan.target) {
        e.preventDefault();
        const sel = plan.target === 'main'
          ? 'textarea[data-composer="main"]'
          : 'textarea[data-composer="sidebar"]';
        const next = document.querySelector(sel);
        if (next instanceof HTMLElement) next.focus();
        return;
      }
      const delta = pageScrollDelta(e.key, 0);
      if (!delta && e.key !== 'PageUp' && e.key !== 'PageDown') return;
      const pane = transcriptNear(document.activeElement);
      if (!pane) return;
      const dy = pageScrollDelta(e.key, pane.clientHeight);
      if (!dy) return;
      e.preventDefault();
      pane.scrollBy(0, dy);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [opts.sidebarComposerVisible]);
}
