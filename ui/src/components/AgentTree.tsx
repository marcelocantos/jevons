// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef, useState } from 'react';
import { CompanyMark } from '../plan/companyMark';
import { modelPrefix } from '../plan/modelPrefix';
import { agentDotState, fleetSecondary, isAsidePurpose } from '../fleet/rowModel';
import { migrateBody, migrateUrl, ModelMenu, type MigrateProvider } from './ModelMenu';
import { repoFromWorkdir, splitSeatNameTarget } from '../frontier/targetHotspot';
import { TargetHotspotTips } from './TargetHotspotTips';

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
  /** 🎯T662: why a not-running seat last stopped, from the daemon's seat-stop ledger. */
  stop_reason?: string;
  /** 🎯T970: being brought up; it has not stopped, so it shows no stop reason. */
  starting?: boolean;
  /** Set when the transcript ends on Cursor's plan wall. Shown on a running seat. */
  plan_wall?: string;
  stopped_at?: string;
  /** 🎯T662: the fleet-wide mass-stop line; identical on every row when present. */
  mass_stop?: string;
  /** 🎯T763: whether a stopped seat can come back. Empty while running. */
  rehydrate?: string;
  reauth_available?: boolean;
};

/** 🎯T662: one alert for the fleet, read off the rows (the daemon puts the same line on each). */
export function massStopLine(agents: AgentRow[]): string {
  for (const a of agents || []) {
    const m = a && a.mass_stop ? String(a.mass_stop).trim() : '';
    if (m) return m;
  }
  return '';
}

const unrecordedStop = 'unknown: process exited and no reason was recorded';

// A relaunch refusal already says why the seat is down. The synthesized
// "no reason was recorded" line beside it only contradicts that.
export function showSeatStopReason(node: AgentRow): boolean {
  if (node.running || node.starting || !node.stop_reason) return false;
  if (node.rehydrate && node.rehydrate !== 'resumable' && node.stop_reason === unrecordedStop) {
    return false;
  }
  return true;
}

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
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<{ top: number; left: number } | null>(null);
  const [options, setOptions] = useState<MigrateProvider[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
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
        {p.version}{p.flavour}
      </sub>
    ) : null;
  async function openMenu(e: { preventDefault: () => void; stopPropagation: () => void; currentTarget: HTMLElement }) {
    e.preventDefault();
    e.stopPropagation();
    const rect = e.currentTarget.getBoundingClientRect();
    const menuH = 240;
    const top = rect.bottom + menuH > window.innerHeight ? Math.max(8, rect.top - menuH) : rect.bottom + 4;
    setAnchor({ top, left: rect.left });
    setOpen(true);
    setError('');
    try {
      const r = await fetch('/api/migrate/options');
      if (!r.ok) throw new Error('could not load providers');
      const data = await r.json();
      setOptions(Array.isArray(data.providers) ? data.providers : []);
    } catch (err) {
      setOptions([]);
      setError(err instanceof Error ? err.message : 'could not load providers');
    }
  }
  async function pick(provider: string, model: string) {
    setBusy(true);
    setError('');
    try {
      const r = await fetch(migrateUrl(node.purpose), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(migrateBody(node.name, node.purpose, provider, model)),
      });
      if (!r.ok) {
        const text = await r.text();
        setError(text.replace(/\s+/g, ' ').trim().slice(0, 180) || 'could not switch');
        return;
      }
      setOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not switch');
    } finally {
      setBusy(false);
    }
  }
  return (
    <span className="model-badge-wrap">
      <button
        type="button"
        className="model-badge"
        data-company={p.company}
        title={p.title}
        aria-label={aria}
        aria-expanded={open}
        onClick={openMenu}
      >
        <CompanyMark company={p.company} />
        {sub}
      </button>
      {open ? (
        <ModelMenu
          options={options || []}
          currentProvider={node.provider}
          anchor={anchor || undefined}
          loading={options === null && !error}
          busy={busy}
          error={error}
          onPick={pick}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </span>
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

function AgentName({ name, workdir }: { name: string; workdir?: string }) {
  const parts = splitSeatNameTarget(name);
  const repo = repoFromWorkdir(workdir);
  if (!parts) return <span className="agent-name">{name}</span>;
  return (
    <span className="agent-name">
      {parts.prefix}
      <span
        className="target-hotspot target-hotspot-finger"
        data-target-id={parts.id}
        data-target-repo={repo}
        role="button"
        tabIndex={0}
      >
        {parts.matched}
      </span>
      {parts.suffix}
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
        <AgentName name={props.node.name} workdir={props.node.workdir} />
        <Secondary node={props.node} parentWorkdir={props.parentWorkdir} />
        {showSeatStopReason(props.node) ? (
          <span className="agent-stop-reason" title={props.node.stop_reason}>
            {'⛔ ' + props.node.stop_reason}
          </span>
        ) : null}
        {props.node.plan_wall ? (
          <span className="agent-plan-wall" title={props.node.plan_wall}>
            {props.node.plan_wall}
          </span>
        ) : null}
        {!props.node.running && props.node.rehydrate && props.node.rehydrate !== 'resumable' ? (
          <span className="agent-rehydrate" title={props.node.rehydrate}>
            <span className="agent-rehydrate-text">{props.node.rehydrate}</span>
          </span>
        ) : null}
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
  const mass = massStopLine(props.agents);
  const rootRef = useRef<HTMLDivElement>(null);
  return (
    <>
      {mass ? (
        <div className="fleet-mass-stop" role="alert" title={mass}>
          {mass}
        </div>
      ) : null}
      <div className="agent-tree-hotspots" ref={rootRef}>
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
      </div>
      <TargetHotspotTips containerRef={rootRef} />
    </>
  );
}
