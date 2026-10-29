// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { ReactNode } from 'react';
import { CoachList } from './CoachList';
import { ReviewPanel, type ReviewItem } from './ReviewPanel';

export type SidebarTab = 'frontier' | 'transcript' | 'coach' | 'review';

const TABS: { id: SidebarTab; label: string }[] = [
  { id: 'frontier', label: 'Frontier' },
  { id: 'transcript', label: 'Transcript' },
  { id: 'coach', label: 'Coach' },
  { id: 'review', label: 'Questions' },
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
  children: ReactNode;
  onAnswerInChat?: (item: ReviewItem) => void;
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
            {/* The ready count belongs to the Frontier tab, so it sits inside that
                tab. At the far end of the bar it read as the Questions count
                ("176 ready" next to Questions). Loading/error notes show in the pane. */}
            {t.id === 'frontier' && typeof props.readyCount === 'number' ? (
              <span className="rhs-tab-count" id="rhs-tab-meta" title={props.readyCount + ' ready leaves'}>
                {props.readyCount}
              </span>
            ) : null}
          </button>
        ))}
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
      <div
        id="review-pane"
        className={'rhs-tab-pane' + (props.tab === 'review' ? ' active' : '')}
        role="tabpanel"
        aria-label="Open owner questions"
      >
        <ReviewPanel active={props.tab === 'review'} onAnswerInChat={props.onAnswerInChat} />
      </div>
      {props.transcript}
    </div>
  );
}
