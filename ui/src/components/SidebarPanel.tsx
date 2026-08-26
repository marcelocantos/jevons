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
  onGraph?: () => void;
  readyCount?: number;
  transcript?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div id="rhs-bottom" aria-label="Frontier and agent transcript">
      <div id="rhs-bottom-tabs" role="tablist">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`rhs-tab-${t.id}`}
            data-tab={t.id}
            className={props.tab === t.id ? 'active' : ''}
            aria-selected={props.tab === t.id}
            onClick={() => props.onTab(t.id)}
          >
            {t.label}
          </button>
        ))}
        <span className="rhs-tab-meta" id="rhs-tab-meta">
          {typeof props.readyCount === 'number' ? props.readyCount + ' ready' : ''}
        </span>
      </div>
      <div
        id="frontier-pane"
        className={'rhs-tab-pane' + (props.tab === 'frontier' ? ' active' : '')}
        role="tabpanel"
      >
        <div id="frontier-toolbar">
          <button type="button" id="frontier-graph" title="Open unachieved dependency graph (~90% view)" onClick={props.onGraph}>
            Graph
          </button>
          <button type="button" id="frontier-refresh">
            Refresh
          </button>
        </div>
        <div id="frontier-body">{props.children}</div>
      </div>
      <div
        id="coach-pane"
        className={'rhs-tab-pane' + (props.tab === 'coach' ? ' active' : '')}
        role="tabpanel"
      >
        <p className="ai-empty">Coach judgments port next.</p>
      </div>
      {props.transcript}
    </div>
  );
}
