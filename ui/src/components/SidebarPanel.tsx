// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';
import { CoachList } from './CoachList';

export type SidebarTab = 'frontier' | 'transcript' | 'coach' | 'reviews';

const TABS: { id: SidebarTab; label: string }[] = [
  { id: 'frontier', label: 'Frontier' },
  { id: 'transcript', label: 'Transcript' },
  { id: 'coach', label: 'Coach' },
  { id: 'reviews', label: 'Reviews' },
];

export function SidebarPanel(props: {
  tab: SidebarTab;
  onTab: (tab: SidebarTab) => void;
  onGraph?: () => void;
  onRefresh?: () => void;
  readyCount?: number;
  /** Shown instead of a count while there is no frontier answer to count. */
  readyNote?: string;
  transcript?: ReactNode;
  reviews?: ReactNode;
  reviewCount?: number;
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
            {t.label}{t.id === 'reviews' && typeof props.reviewCount === 'number' ? ` (${props.reviewCount})` : ''}
          </button>
        ))}
        <span className="rhs-tab-meta" id="rhs-tab-meta">
          {props.readyNote || (typeof props.readyCount === 'number' ? props.readyCount + ' ready' : '')}
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
          <button type="button" id="frontier-refresh" onClick={props.onRefresh}>
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
        <CoachList active={props.tab === 'coach'} />
      </div>
      <div id="reviews-pane" className={'rhs-tab-pane' + (props.tab === 'reviews' ? ' active' : '')} role="tabpanel">{props.reviews}</div>
      {props.transcript}
    </div>
  );
}
