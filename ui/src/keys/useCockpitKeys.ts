// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect } from 'react';
import { tryFocusComposer } from './composerFocus';
import { isSidebarComposerFocusable, planComposerTabCycle } from './composerTab';
import { applyTranscriptPageKey } from './pageScroll';

function composerKind(el: Element | null): 'main' | 'sidebar' | 'other' {
  if (!el) return 'other';
  const box = el.closest('textarea[data-composer]');
  if (!box) return 'other';
  const kind = box.getAttribute('data-composer');
  if (kind === 'main' || kind === 'sidebar') return kind;
  return 'other';
}

export function useCockpitKeys(): void {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const active = composerKind(document.activeElement);
      const plan = planComposerTabCycle(e, {
        active,
        sidebarVisible: isSidebarComposerFocusable(document),
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
      const main = document.querySelector('textarea[data-composer="main"]');
      const slash = tryFocusComposer(e, main instanceof HTMLElement ? main : null, document.activeElement);
      if (slash.didFocus) {
        e.preventDefault();
        return;
      }
      if (e.key !== 'PageUp' && e.key !== 'PageDown') return;
      if (applyTranscriptPageKey(e.key, document.activeElement)) e.preventDefault();
    };
    // Capture so Tab is claimed before document-order chrome (T549 / T571).
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, []);
}
