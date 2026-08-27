// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { CompanyMark } from '../plan/companyMark';
import { modelPrefix } from '../plan/modelPrefix';
import { pixelFixtureActive } from '../visual/oldCockpitFixture';
import { agentDotState, fleetSecondary, isAsidePurpose } from '../fleet/rowModel';

export type AgentRow = {
  name: string;
  purpose?: string;
  parent?: string;
  status?: string;
  running?: boolean;
  phase?: string;
  step?: string;
  progress?: string;
  provider?: string;
  model?: string;
  workdir?: string;
  target_id?: string;
  ledger?: string;
};

export type AgentNode = AgentRow & { children: AgentNode[] };

export function buildAgentForest(agents: AgentRow[]): AgentNode[] {
  const byName = new Map<string, AgentNode>();
  for (const a of agents) byName.set(a.name, { ...a, children: [] });
  const roots: AgentNode[] = [];
  for (const n of byName.values()) {
    const p = n.parent && byName.get(n.parent);
    if (p && p.name !== n.name) p.children.push(n);
    else roots.push(n);
  }
  const sort = (xs: AgentNode[]) => {
    xs.sort((a, b) => a.name.localeCompare(b.name));
    xs.forEach((c) => sort(c.children));
  };
  sort(roots);
  return roots;
}

function ModelBadge({ node }: { node: AgentNode }) {
  const p = modelPrefix(node);
  if (!p.company) return null;
  const aria =
    'Select provider and model' +
    (node.name ? ' for ' + node.name : '') +
    '. Current: ' +
    p.title;
  const sub =
    p.initial || p.version ? (
      <sub>
        {p.initial ? <span className="model-family">{p.initial}</span> : null}
        {p.version}
      </sub>
    ) : null;
  return (
    <button
      type="button"
      className="model-badge"
      data-company={p.company}
      title={p.title}
      aria-label={aria}
    >
      {pixelFixtureActive() && p.company === 'anthropic' ? (
        <svg
          className="model-icon"
          data-mark="claude-splat"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <circle
            cx="12"
            cy="12"
            r="7.2"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
          />
          <line
            x1="7.2"
            y1="16.8"
            x2="16.8"
            y2="7.2"
            stroke="currentColor"
            strokeWidth="2.2"
          />
        </svg>
      ) : (
        <CompanyMark company={p.company} />
      )}
      {sub}
    </button>
  );
}

function githubDir(workdir?: string) {
  const s = String(workdir || '');
  const gh = /github\.com\/(.+)$/.exec(s);
  if (!gh) return null;
  return (
    <span className="agent-dir">
      <svg className="gh-icon" viewBox="0 0 16 16" aria-hidden="true">
        <path
          fill="currentColor"
          d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0016 8c0-4.42-3.58-8-8-8z"
        />
      </svg>
      {gh[1]}
    </span>
  );
}

function Secondary(props: { node: AgentNode; parentWorkdir?: string }) {
  const sec = fleetSecondary(props.node, {
    parentWorkdir: props.parentWorkdir,
    hasChildren: props.node.children.length > 0,
  });
  if (!sec.kind || !sec.text) return null;
  if (sec.kind === 'path') return githubDir(props.node.workdir);
  return <span className={'agent-dir agent-' + sec.kind}>{sec.text}</span>;
}

function Row(props: {
  node: AgentNode;
  depth: number;
  selected: string;
  onSelect: (name: string) => void;
  onDismiss?: (name: string) => void;
  parentWorkdir?: string;
}) {
  const dot = agentDotState(props.node);
  // 🎯T269: hover-gated dismiss × only on purpose=aside rows (not work/PO/portfolio).
  const isAside = props.node.purpose !== 'portfolio' && isAsidePurpose(props.node.purpose);
  return (
    <>
      <div
        className={
          'agent-node' +
          (props.node.purpose === 'portfolio' ? ' agent-portfolio' : '') +
          (isAside ? ' agent-aside' : '') +
          (props.node.name === props.selected ? ' selected' : '')
        }
        onClick={() => {
          if (props.node.purpose !== 'portfolio') props.onSelect(props.node.name);
        }}
      >
        {props.node.purpose === 'portfolio' ? (
          <span className="agent-folder" aria-hidden>
            📁
          </span>
        ) : (
          <span className={'agent-dot ' + dot} />
        )}
        {props.node.purpose !== 'portfolio' ? <ModelBadge node={props.node} /> : null}
        <span className="agent-name">{props.node.name}</span>
        <Secondary node={props.node} parentWorkdir={props.parentWorkdir} />
        {isAside ? (
          <button
            type="button"
            className="agent-dismiss"
            data-agent-dismiss={props.node.name}
            aria-label={'Dismiss aside ' + props.node.name}
            title="Dismiss"
            onClick={(e) => {
              // × → DELETE /api/asides; never selects the row.
              e.preventDefault();
              e.stopPropagation();
              props.onDismiss?.(props.node.name);
            }}
          >
            ×
          </button>
        ) : null}
      </div>
      {props.node.children.length ? (
        <div className="agent-children">
          {props.node.children.map((c) => (
            <Row
              key={c.name}
              node={c}
              depth={props.depth + 1}
              selected={props.selected}
              onSelect={props.onSelect}
              onDismiss={props.onDismiss}
              parentWorkdir={props.node.workdir}
            />
          ))}
        </div>
      ) : null}
    </>
  );
}

export function AgentTree(props: {
  agents: AgentRow[];
  selected: string;
  onSelect: (name: string) => void;
  onDismiss?: (name: string) => void;
}) {
  const roots = buildAgentForest(props.agents);
  return (
    <>
      {roots.map((n) => (
        <Row
          key={n.name}
          node={n}
          depth={0}
          selected={props.selected}
          onSelect={props.onSelect}
          onDismiss={props.onDismiss}
        />
      ))}
    </>
  );
}
