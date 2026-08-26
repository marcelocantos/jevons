// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  expireCardCache,
  formatFanout,
  formatStatus,
  hoverCardMarkdown,
  shortName,
  type FrontierRow,
  type HoverCardCache,
} from '../frontier/table';
import {
  addKickoffSubmitted,
  applyEngagement,
  applyKickoffSubmitted,
  playChromeSpec,
  playKickoffRequest,
  pruneKickoffSubmitted,
  removeKickoffSubmitted,
  stopEngagementRequest,
  type KickoffSubmittedSet,
  type PlayAgent,
  type PlayRow,
} from '../frontier/play';
import { InstantTip } from './InstantTip';
import { rowMatchesHighlight } from '../frontier/targetAsk';
import { TargetHoverCard } from './TargetHoverCard';

export type { FrontierRow };

export type FrontierFetch = (url: string, init: { method: 'POST'; headers: Record<string, string>; body: string }) => Promise<{ ok: boolean; status: number }>;

const defaultFetch: FrontierFetch = (url, init) => fetch(url, init);

function FanCell(props: { row: FrontierRow }) {
  const fan = formatFanout(props.row.fanout, props.row.id, props.row.dependents);
  return <td className={fan.visible ? 'ft-fanout' : 'ft-fanout ft-fanout-empty'}>{fan.text}</td>;
}

/** 🎯T182 / T198 / T278: play → submitted spinner → engaged Stop. */
function PlayCell(props: {
  row: PlayRow;
  agents: PlayAgent[];
  selectedAgent: string;
  onPlay: (row: PlayRow) => void;
  onStop: (row: PlayRow) => void;
}) {
  const spec = playChromeSpec(props.row, { agents: props.agents, selectedAgent: props.selectedAgent });
  return (
    <td className="ft-play">
      <button
        type="button"
        className={spec.className}
        aria-label={spec.ariaLabel}
        title={spec.title}
        disabled={spec.disabled}
        data-play-mode={spec.mode}
        onClick={() => (spec.mode === 'stop' ? props.onStop(props.row) : spec.mode === 'play' ? props.onPlay(props.row) : undefined)}
      >
        {spec.spinning ? <span className="ft-spin" aria-hidden="true" /> : spec.glyph}
      </button>
    </td>
  );
}

function FrontierRowView(props: {
  row: PlayRow;
  cache: HoverCardCache;
  agents: PlayAgent[];
  selectedAgent: string;
  highlighted: boolean;
  onPlay: (row: PlayRow) => void;
  onStop: (row: PlayRow) => void;
}) {
  const [nameEl, setNameEl] = useState<HTMLTableCellElement | null>(null);
  const md = hoverCardMarkdown(props.cache, props.row);
  const engaged = props.row.engaged ? props.row.engaged_agents || [] : [];
  // 🎯T267: the target-ask row is emphasized and scrolled into view.
  const trRef = useRef<HTMLTableRowElement>(null);
  useEffect(() => {
    if (!props.highlighted) return;
    const el = trRef.current;
    if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ block: 'nearest' });
  }, [props.highlighted]);
  const trClass = [engaged.length ? 'ft-engaged' : '', props.highlighted ? 'ft-highlight' : ''].filter(Boolean).join(' ');
  return (
    <tr
      ref={trRef}
      className={trClass || undefined}
      data-target-id={props.row.id}
      data-engaged-agents={engaged.length ? engaged.join(',') : undefined}
      data-frontier-highlight={props.highlighted ? '1' : undefined}
      aria-selected={props.highlighted ? true : undefined}
    >
      <td className="ft-id">
        <InstantTip
          groupHosts={() => [nameEl]}
          clampSelectors={['#frontier-table', '#frontier-body']}
          content={<TargetHoverCard markdown={md} id={props.row.id} name={props.row.name} />}
        >
          {'🎯' + props.row.id}
        </InstantTip>
      </td>
      <td className="ft-name" ref={setNameEl}>
        {shortName(props.row.name, 72)}
      </td>
      <td className="ft-status">{formatStatus(props.row.status)}</td>
      <FanCell row={props.row} />
      <PlayCell row={props.row} agents={props.agents} selectedAgent={props.selectedAgent} onPlay={props.onPlay} onStop={props.onStop} />
    </tr>
  );
}

export function FrontierTable(props: {
  rows: FrontierRow[];
  agents?: PlayAgent[];
  selectedAgent?: string;
  /** 🎯T267: target id to emphasize (target-ask focus). */
  highlightId?: string;
  ledgerKey?: string;
  fetcher?: FrontierFetch;
  onNotice?: (text: string) => void;
}) {
  const cacheRef = useRef<HoverCardCache>({});
  useEffect(() => {
    expireCardCache(cacheRef.current, props.rows);
  }, [props.rows]);
  const agents = props.agents || [];
  const selectedAgent = props.selectedAgent || '';
  const fetcher = props.fetcher || defaultFetch;
  const [submitted, setSubmitted] = useState<KickoffSubmittedSet>({});
  const engagedRows = useMemo(() => applyEngagement(props.rows, agents, props.ledgerKey), [props.rows, agents, props.ledgerKey]);
  useEffect(() => {
    setSubmitted((s) => pruneKickoffSubmitted(s, engagedRows));
  }, [engagedRows]);
  const rows = useMemo(() => applyKickoffSubmitted(engagedRows, submitted), [engagedRows, submitted]);
  const notice = props.onNotice;

  const onPlay = useCallback(
    (row: PlayRow) => {
      const req = playKickoffRequest(row, { agents, selectedAgent });
      if (req.blocked) {
        notice?.(req.message);
        return;
      }
      // 🎯T278: submitted chrome lands before the PO answers.
      setSubmitted((s) => addKickoffSubmitted(s, row.id));
      fetcher(req.url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(req.body) })
        .then((r) => {
          if (!r.ok) {
            setSubmitted((s) => removeKickoffSubmitted(s, row.id));
            notice?.('Kickoff failed: HTTP ' + r.status);
          }
        })
        .catch((err) => {
          setSubmitted((s) => removeKickoffSubmitted(s, row.id));
          notice?.('Kickoff failed: ' + String(err instanceof Error ? err.message : err));
        });
    },
    [agents, selectedAgent, fetcher, notice],
  );

  const onStop = useCallback(
    (row: PlayRow) => {
      const req = stopEngagementRequest(row.id, props.ledgerKey);
      fetcher(req.url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(req.body) })
        .then((r) => {
          if (!r.ok) notice?.('Stop failed: HTTP ' + r.status);
        })
        .catch((err) => notice?.('Stop failed: ' + String(err instanceof Error ? err.message : err)));
    },
    [props.ledgerKey, fetcher, notice],
  );

  return (
    <table id="frontier-table" aria-label="Bullseye frontier">
      <tbody>
        {rows.map((r) => (
          <FrontierRowView
            key={r.id}
            row={r}
            cache={cacheRef.current}
            agents={agents}
            selectedAgent={selectedAgent}
            highlighted={rowMatchesHighlight(r.id, props.highlightId)}
            onPlay={onPlay}
            onStop={onStop}
          />
        ))}
      </tbody>
    </table>
  );
}
