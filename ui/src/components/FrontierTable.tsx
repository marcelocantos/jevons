// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  expireCardCache,
  formatFanout,
  formatStatus,
  hoverCardMarkdown,
  normalizeTargetID,
  shortName,
  type FrontierRow,
  type HoverCardCache,
} from '../frontier/table';
import {
  addKickoffSubmitted,
  applyEngagement,
  applyKickoffAcked,
  applyKickoffSubmitted,
  applySeatWaits,
  SEAT_WAITS_PATH,
  STOP_GLYPH,
  playChromeSpec,
  playKickoffRequest,
  pruneKickoffSubmitted,
  removeKickoffSubmitted,
  stopEngagementRequest,
  type KickoffSubmittedSet,
  type PlayAgent,
  type PlayRow,
  type SeatWaits,
} from '../frontier/play';
import { InstantTip } from './InstantTip';
import { rowMatchesHighlight } from '../frontier/targetAsk';
import { TargetHoverCard } from './TargetHoverCard';

export type { FrontierRow };

export type FrontierFetch = (url: string, init: { method: 'POST'; headers: Record<string, string>; body: string }) => Promise<{ ok: boolean; status: number }>;

const defaultFetch: FrontierFetch = (url, init) => fetch(url, init);

/** 🎯T980: loads GET /api/seat-waits; tests inject their own. */
export type SeatWaitsLoader = () => Promise<SeatWaits | null>;
const defaultSeatWaits: SeatWaitsLoader = () =>
  fetch(SEAT_WAITS_PATH).then((r) => (r.ok ? (r.json() as Promise<SeatWaits>) : null));

function FanCell(props: { row: FrontierRow }) {
  const fan = formatFanout(props.row.fanout, props.row.id, props.row.dependents);
  return <td className={fan.visible ? 'ft-fanout' : 'ft-fanout ft-fanout-empty'}>{fan.text}</td>;
}

/** 🎯T182 / T198 / T278 / T980: play → in flight → acknowledged → red
 * arrow when no plan can seat it (click force-seats; hover offers stop to
 * its left) → engaged Stop. */
function PlayCell(props: {
  row: PlayRow;
  agents: PlayAgent[];
  selectedAgent: string;
  onPlay: (row: PlayRow) => void;
  onStop: (row: PlayRow) => void;
  onForce: (row: PlayRow) => void;
}) {
  const spec = playChromeSpec(props.row, { agents: props.agents, selectedAgent: props.selectedAgent });
  const [hover, setHover] = useState(false);
  const waiting = spec.mode === 'waiting';
  const onClick = () => {
    if (spec.mode === 'stop') props.onStop(props.row);
    else if (spec.mode === 'play') props.onPlay(props.row);
    else if (waiting) {
      setHover(false);
      props.onForce(props.row);
    }
  };
  return (
    <td className="ft-play">
      <div
        className="ft-play-wrap"
        onMouseEnter={waiting ? () => setHover(true) : undefined}
        onMouseLeave={waiting ? () => setHover(false) : undefined}
      >
        {waiting && hover ? (
          <button
            type="button"
            className="ft-play-btn ft-stop-btn ft-wait-stop"
            aria-label={'Stop the request for 🎯' + props.row.id}
            title="Stop: withdraw this request"
            onClick={() => {
              setHover(false);
              props.onStop(props.row);
            }}
          >
            {STOP_GLYPH}
          </button>
        ) : null}
        <button
          type="button"
          className={spec.className}
          aria-label={spec.ariaLabel}
          title={spec.title}
          disabled={spec.disabled}
          data-play-mode={spec.mode}
          onClick={onClick}
        >
          {spec.spinning ? <span className="ft-spin" aria-hidden="true" /> : spec.glyph}
        </button>
      </div>
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
  onForce: (row: PlayRow) => void;
}) {
  const [idEl, setIdEl] = useState<HTMLTableCellElement | null>(null);
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
      <td className="ft-id" ref={setIdEl}>
        <InstantTip
          groupHosts={() => [idEl]}
          placement="left-of-host"
          clampSelectors={['#frontier-table', '#frontier-body']}
          content={<TargetHoverCard markdown={md} id={props.row.id} name={props.row.name} />}
        >
          {'🎯' + props.row.id}
        </InstantTip>
      </td>
      <td className="ft-name">
        {shortName(props.row.name, 72)}
      </td>
      <td className="ft-status">{formatStatus(props.row.status)}</td>
      <FanCell row={props.row} />
      <PlayCell row={props.row} agents={props.agents} selectedAgent={props.selectedAgent} onPlay={props.onPlay} onStop={props.onStop} onForce={props.onForce} />
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
  seatWaits?: SeatWaitsLoader;
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
  // 🎯T980: kickoffs the PO has acknowledged, and the server's waits for a
  // seat. A forced wait is hidden until a newer refusal or an engagement.
  const [acked, setAcked] = useState<KickoffSubmittedSet>({});
  const [waits, setWaits] = useState<SeatWaits>({});
  const [forced, setForced] = useState<Record<string, string>>({});
  const engagedRows = useMemo(() => applyEngagement(props.rows, agents, props.ledgerKey), [props.rows, agents, props.ledgerKey]);
  useEffect(() => {
    setSubmitted((s) => pruneKickoffSubmitted(s, engagedRows));
    setAcked((s) => pruneKickoffSubmitted(s, engagedRows));
  }, [engagedRows]);
  const loadWaits = props.seatWaits || defaultSeatWaits;
  useEffect(() => {
    let live = true;
    const poll = () =>
      loadWaits()
        .then((w) => {
          if (live && w && typeof w === 'object') setWaits(w);
        })
        .catch(() => {});
    void poll();
    const t = setInterval(poll, 10_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [loadWaits]);
  const visibleWaits = useMemo(() => {
    const out: SeatWaits = {};
    for (const [k, w] of Object.entries(waits)) {
      if (forced[k] && forced[k] === String(w.at || '')) continue;
      out[k] = w;
    }
    return out;
  }, [waits, forced]);
  const rows = useMemo(
    () => applySeatWaits(applyKickoffAcked(applyKickoffSubmitted(engagedRows, submitted), acked), visibleWaits),
    [engagedRows, submitted, acked, visibleWaits],
  );
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
            return;
          }
          // 🎯T980: delivered to the PO — acknowledged.
          setAcked((s) => addKickoffSubmitted(s, row.id));
        })
        .catch((err) => {
          setSubmitted((s) => removeKickoffSubmitted(s, row.id));
          notice?.('Kickoff failed: ' + String(err instanceof Error ? err.message : err));
        });
    },
    [agents, selectedAgent, fetcher, notice],
  );

  const onForce = useCallback(
    (row: PlayRow) => {
      const req = playKickoffRequest(row, { agents, selectedAgent, force: true });
      if (req.blocked) {
        notice?.(req.message);
        return;
      }
      const id = normalizeTargetID(row.id);
      setForced((f) => ({ ...f, [id]: String(waits[id]?.at || '') }));
      setAcked((s) => removeKickoffSubmitted(s, row.id));
      setSubmitted((s) => addKickoffSubmitted(s, row.id));
      fetcher(req.url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(req.body) })
        .then((r) => {
          if (!r.ok) {
            setSubmitted((s) => removeKickoffSubmitted(s, row.id));
            notice?.('Force-play failed: HTTP ' + r.status);
            return;
          }
          setAcked((s) => addKickoffSubmitted(s, row.id));
        })
        .catch((err) => {
          setSubmitted((s) => removeKickoffSubmitted(s, row.id));
          notice?.('Force-play failed: ' + String(err instanceof Error ? err.message : err));
        });
    },
    [agents, selectedAgent, fetcher, notice, waits],
  );

  const onStop = useCallback(
    (row: PlayRow) => {
      // 🎯T980: stopping a waiting request withdraws it here at once too.
      const id = normalizeTargetID(row.id);
      setSubmitted((s) => removeKickoffSubmitted(s, row.id));
      setAcked((s) => removeKickoffSubmitted(s, row.id));
      setWaits((w) => {
        if (!w[id]) return w;
        const out = { ...w };
        delete out[id];
        return out;
      });
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
            onForce={onForce}
          />
        ))}
      </tbody>
    </table>
  );
}
