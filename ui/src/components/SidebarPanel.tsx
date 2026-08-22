// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';

export type SidebarTab = 'frontier' | 'transcript' | 'coach';

const TABS: { id: SidebarTab; label: string }[] = [
  { id: 'frontier', label: 'Frontier' },
  { id: 'transcript', label: 'Transcript' },
  { id: 'coach', label: 'Coach' },
];

export function SidebarPanel(props: {
  tab: SidebarTab;
  onTab: (tab: SidebarTab) => void;
  children: ReactNode;
}) {
  return (
    <div className="sidebar-panel">
      <div className="sidebar-tabs" role="tablist">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            className={props.tab === t.id ? 'selected' : ''}
            aria-selected={props.tab === t.id}
            onClick={() => props.onTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div className="sidebar-tab-body">{props.children}</div>
    </div>
  );
}
