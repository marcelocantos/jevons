// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';

export type SidebarTab = 'transcript' | 'frontier' | 'more';

export function SidebarPanel(props: {
  tab: SidebarTab;
  onTab: (tab: SidebarTab) => void;
  children: ReactNode;
}) {
  return (
    <div className="sidebar-panel">
      <div className="sidebar-tabs">
        {(['transcript', 'frontier', 'more'] as SidebarTab[]).map((t) => (
          <button
            key={t}
            type="button"
            className={props.tab === t ? 'selected' : ''}
            onClick={() => props.onTab(t)}
          >
            {t}
          </button>
        ))}
      </div>
      <div className="sidebar-tab-body">{props.children}</div>
    </div>
  );
}
