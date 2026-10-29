// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { CompanyMark } from '../plan/companyMark';
import { modelPrefix } from '../plan/modelPrefix';
import { agentDotState, fleetSecondary, isAsidePurpose } from '../fleet/rowModel';
import { ModelMenu, SWITCH_MODEL_URL, switchModelBody, type ProviderOption } from './ModelMenu';
import { splitSeatNameTarget } from '../frontier/targetHotspot';
import { TargetHotspotTips } from './TargetHotspotTips';

export type AgentRow = {
  name: string;
  /** 🎯T1304: painted before /api/agents answered; nothing is known about it. */
  placeholder?: boolean;
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
  /** Trusted seat-stop ledger Actor or fleet-intent By, not inferred from reason text. */
  stop_actor?: string;
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
  /** 🎯T1121: live | park | fault | history — history rows hide unless toggled. */
  class?: string;
  visible?: boolean;
  panel_line?: string;
  /** 🎯T981: owner paused this seat from the cockpit; nudges are suppressed. */
  owner_paused?: boolean;
  /** 🎯T845: provider request failure latched on the seat. */
  failure_class?: string;
  last_error?: string;
};

/** 🎯T662: one alert for the fleet, read off the rows (the daemon puts the same line on each). */

export function panelRowShown(a: AgentRow, showHistory: boolean): boolean {
  if (showHistory) return true;
  if (a.visible === false) return false;
  if (a.class === 'history') return false;
  return true;
}

export function filterPanelAgents(agents: AgentRow[], showHistory: boolean): AgentRow[] {
  return (agents || []).filter((a) => panelRowShown(a, showHistory));
}

/**
 * The GET /api/agents body as panel rows. Every field the daemon sends is
 * kept: a hand-copied field list dropped class/visible/panel_line once
 * (fc352eab), and the 🎯T1121 filter then showed every reaped row.
 */
export function agentRowsFromAPI(data: unknown): AgentRow[] {
  const list = Array.isArray(data) ? data : [];
  return list
    .filter((a): a is AgentRow => !!a && typeof a === 'object' && typeof (a as AgentRow).name === 'string' && (a as AgentRow).name !== '')
    .map((a) => ({ ...a }));
}

const FLEET_HISTORY_KEY = 'fleet-panel-history';

function readFleetHistory(): boolean {
  try {
    return typeof window !== 'undefined' && window.localStorage.getItem(FLEET_HISTORY_KEY) === '1';
  } catch {
    return false;
  }
}

function writeFleetHistory(on: boolean) {
  try {
    window.localStorage.setItem(FLEET_HISTORY_KEY, on ? '1' : '0');
  } catch {
    // Storage blocked: the toggle still works for this page.
  }
}

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
  const [options, setOptions] = useState<ProviderOption[] | null>(null);
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
    if (open) {
      setOpen(false);
      return;
    }
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
      const r = await fetch(SWITCH_MODEL_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(switchModelBody(node.name, provider, model)),
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

function AgentName({ name }: { name: string }) {
  const parts = splitSeatNameTarget(name);
  if (!parts) return <span className="agent-name">{name}</span>;
  return (
    <span className="agent-name">
      {parts.prefix}
      <span
        className="target-hotspot target-hotspot-finger"
        data-target-id={parts.id}
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

/** A stop is a compact marker, not a second line of text in every fleet row. */
function StopDetails({ node }: { node: AgentNode }) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<{ top: number; left: number } | null>(null);
  const popup = useRef<HTMLDivElement>(null);
  const wrapper = useRef<HTMLSpanElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const popupId = useId();
  useEffect(() => {
    if (!open) return;
    const outside = (e: PointerEvent) => {
      if (!wrapper.current?.contains(e.target as Node)) setOpen(false);
    };
    const escape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        setOpen(false);
        button.current?.focus();
      }
    };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('pointerdown', outside);
      document.removeEventListener('keydown', escape);
    };
  }, [open]);
  // Measure the actual popup after layout, not a guessed height: long reasons
  // must remain inside even a 320×568 viewport. Reposition when the viewport
  // changes or the fleet list scrolls while the disclosure is open.
  useLayoutEffect(() => {
    if (!open) return;
    const position = () => {
      if (!button.current || !popup.current) return;
      const trigger = button.current.getBoundingClientRect();
      const box = popup.current.getBoundingClientRect();
      const margin = 8;
      const height = Math.min(box.height, window.innerHeight - 2 * margin);
      const below = window.innerHeight - margin - trigger.bottom;
      const above = trigger.top - margin;
      const preferred = below >= height || below >= above ? trigger.bottom + 4 : trigger.top - height - 4;
      setAnchor({
        top: Math.max(margin, Math.min(preferred, window.innerHeight - margin - height)),
        left: Math.max(margin, Math.min(trigger.left, window.innerWidth - margin - box.width)),
      });
    };
    position();
    window.addEventListener('resize', position);
    // Capture scroll events on the nested fleet scroller too.
    window.addEventListener('scroll', position, true);
    return () => {
      window.removeEventListener('resize', position);
      window.removeEventListener('scroll', position, true);
    };
  }, [open, node.stop_reason, node.stopped_at]);
  if (!showSeatStopReason(node)) return null;
  const reason = node.stop_reason || '';
  return (
    <span className="agent-stop-wrap" ref={wrapper}>
      <button
        ref={button}
        type="button"
        className="agent-stop-icon"
        aria-label={'Stop details for ' + node.name}
        aria-expanded={open}
        aria-controls={open ? popupId : undefined}
        onClick={(e) => {
          e.stopPropagation();
          setAnchor(null);
          setOpen(!open);
        }}
      >⛔</button>
      {open ? (
        <div
          id={popupId}
          ref={popup}
          className="agent-stop-popup"
          role="dialog"
          aria-label={'Stop details for ' + node.name}
          style={anchor ? { top: anchor.top, left: anchor.left } : { top: 0, left: 0, visibility: 'hidden' }}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="agent-stop-popup-head">
            <strong>{node.name}</strong>
            <button type="button" onClick={() => { setOpen(false); button.current?.focus(); }}>Close</button>
          </div>
          <div className="agent-stop-popup-body">
            <div>Reason: {reason}</div>
            {node.stop_actor ? <div>Actor: {node.stop_actor}</div> : null}
            {node.stopped_at ? <div>Since: {node.stopped_at}</div> : null}
          </div>
        </div>
      ) : null}
    </span>
  );
}

function SeatPauseControl(props: { node: AgentNode; onPause?: (name: string, pause: boolean) => void }) {
  if (props.node.purpose === 'portfolio' || props.node.name === 'jevons') return null;
  const paused = !!props.node.owner_paused;
  const canPause = props.node.running || paused;
  if (!canPause || !props.onPause) return null;
  return (
    <button
      type="button"
      className={'agent-pause-btn' + (paused ? ' agent-pause-btn-active' : '')}
      aria-label={paused ? 'Unpause ' + props.node.name : 'Pause ' + props.node.name}
      title={paused ? 'Unpause — resume where it left off' : 'Pause — interrupt this seat only (❚❚)'}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        props.onPause?.(props.node.name, !paused);
      }}
    >
      {paused ? '▶' : '❚❚'}
    </button>
  );
}

/**
 * 🎯T1122: an idle leaf worker is not a decision, a fault or a change in what
 * the owner would do next, so it does not earn its own row. A parent's idle,
 * running, live leaf workers fold into one "N idle" line (2026-10-22: 71 idle
 * workers made a 107-row panel). Parked leaf workers (class park) fold into
 * the same line: an owner decision arrives as a typed question on the
 * Questions tab, not as a parked row, and 57 parked rows buried the tree the
 * same afternoon. Working and faulted seats, seats with children, the
 * selected seat and owner-paused seats always show.
 */
export function partitionIdleChildren(
  children: AgentNode[],
  selected: string,
): { shown: AgentNode[]; idle: AgentNode[]; parked: AgentNode[] } {
  const shown: AgentNode[] = [];
  const idle: AgentNode[] = [];
  const parked: AgentNode[] = [];
  for (const c of children) {
    const leafWorker =
      c.children.length === 0 &&
      c.name !== selected &&
      !c.owner_paused &&
      c.purpose !== 'portfolio' &&
      !c.name.endsWith('-po');
    if (leafWorker && c.running === true && (c.phase === 'idle' || c.progress === 'idle') && (c.class === undefined || c.class === 'live')) {
      idle.push(c);
    } else if (leafWorker && c.running !== true && c.class === 'park') {
      parked.push(c);
    } else {
      shown.push(c);
    }
  }
  return { shown, idle, parked };
}

function foldLabel(idle: number, parked: number, open: boolean): string {
  const parts = [idle ? idle + ' idle' : '', parked ? parked + ' parked' : ''].filter(Boolean).join(', ');
  return open ? 'Hide ' + parts : parts + ' — show';
}

function Row(props: {
  node: AgentNode;
  depth: number;
  selected: string;
  onSelect: (name: string) => void;
  onDismiss?: (name: string) => void;
  onPause?: (name: string, pause: boolean) => void;
  onReauth?: (name: string) => Promise<void>;
  parentWorkdir?: string;
}) {
  // 🎯T1304: a placeholder row has no liveness yet; its dot stays blank.
  const dot = props.node.placeholder ? '' : agentDotState(props.node);
  const [reauthBusy, setReauthBusy] = useState(false);
  const [reauthError, setReauthError] = useState('');
  // 🎯T269: hover-gated dismiss × only on purpose=aside rows (not work/PO/portfolio).
  const isAside = props.node.purpose !== 'portfolio' && isAsidePurpose(props.node.purpose);
  const [showIdle, setShowIdle] = useState(false);
  const { shown, idle, parked } = partitionIdleChildren(props.node.children, props.selected);
  const visibleChildren = showIdle ? props.node.children : shown;
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
        <AgentName name={props.node.name} />
        <SeatPauseControl node={props.node} onPause={props.onPause} />
        <StopDetails node={props.node} />
        <Secondary node={props.node} parentWorkdir={props.parentWorkdir} />
        {props.node.panel_line ? (
          <span className="agent-panel-line" title={props.node.panel_line}>
            {props.node.panel_line /* the ⛔ disclosure beside the name already marks the row */}
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
            {props.node.reauth_available && props.onReauth ? (
              <button
                type="button"
                className="agent-reauth"
                disabled={reauthBusy}
                aria-label={'Reauth ' + props.node.name}
                onClick={async (e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  if (reauthBusy) return;
                  setReauthBusy(true);
                  setReauthError('');
                  try {
                    await props.onReauth?.(props.node.name);
                  } catch (err) {
                    setReauthError(err instanceof Error ? err.message : 'Authentication recovery failed');
                  } finally {
                    setReauthBusy(false);
                  }
                }}
              >
                {reauthBusy ? 'Recovering…' : 'Reauth'}
              </button>
            ) : null}
            {reauthError ? <span className="agent-reauth-error" role="alert">{reauthError}</span> : null}
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
          {visibleChildren.map((c) => (
            <Row
              key={c.name}
              node={c}
              depth={props.depth + 1}
              selected={props.selected}
              onSelect={props.onSelect}
              onDismiss={props.onDismiss}
              onPause={props.onPause}
              onReauth={props.onReauth}
              parentWorkdir={props.node.workdir}
            />
          ))}
          {idle.length + parked.length ? (
            <button
              type="button"
              className="agent-idle-fold"
              aria-expanded={showIdle}
              onClick={(e) => {
                e.stopPropagation();
                setShowIdle((v) => !v);
              }}
            >
              {foldLabel(idle.length, parked.length, showIdle)}
            </button>
          ) : null}
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
  onPause?: (name: string, pause: boolean) => void;
  onReauth?: (name: string) => Promise<void>;
  showHistory?: boolean;
  onToggleHistory?: () => void;
}) {
  // The tree owns the history toggle unless a caller controls it, so the
  // 🎯T1121 filter cannot be unwired by a caller that forgets the props.
  const [ownHistory, setOwnHistory] = useState(readFleetHistory);
  const showHistory = props.showHistory ?? ownHistory;
  const toggleHistory = props.onToggleHistory ?? (() => {
    setOwnHistory((on) => {
      writeFleetHistory(!on);
      return !on;
    });
  });
  const visibleAgents = filterPanelAgents(props.agents, showHistory);
  const roots = buildAgentForest(visibleAgents);
  const mass = massStopLine(props.agents);
  const rootRef = useRef<HTMLDivElement>(null);
  return (
    <>
      <div className="fleet-panel-toolbar">
        <button
          type="button"
          className="fleet-history-toggle"
          aria-pressed={showHistory}
          title="Show reaped and finished seats"
          onClick={toggleHistory}
        >
          {showHistory ? "Hide history" : "History"}
        </button>
      </div>
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
            onPause={props.onPause}
            onReauth={props.onReauth}
          />
        ))}
      </div>
      <TargetHotspotTips containerRef={rootRef} />
    </>
  );
}
