// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';

// 🎯T354: the coach tab lists judgments the daemon already stored.
// A bare list is the v1; filing still happens in the overseer chat.

type Judgment = {
  fingerprint: string;
  name?: string;
  observation?: string;
  severity?: string;
  delivered_at?: string;
  disposition?: string;
  reason?: string;
  target_id?: string;
  evidence?: string;
  mode?: string;
};

type Payload = {
  judgments?: Judgment[];
  total?: number;
  pending?: number;
};

function when(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

function chip(disposition?: string): string {
  const d = (disposition || 'pending').trim() || 'pending';
  return d === 'pending' ? 'coach-chip coach-pending' : 'coach-chip';
}

export function CoachList(props: { active: boolean }) {
  const [rows, setRows] = useState<Judgment[] | null>(null);
  const [pending, setPending] = useState(0);
  const [total, setTotal] = useState(0);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    try {
      const r = await fetch('/api/rsi/dispositions');
      if (!r.ok) throw new Error('coach list unavailable');
      const body = (await r.json()) as Payload;
      const list = [...(body.judgments || [])];
      list.sort((a, b) => (b.delivered_at || '').localeCompare(a.delivered_at || ''));
      setRows(list);
      setPending(body.pending || 0);
      setTotal(body.total ?? list.length);
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'coach list unavailable');
    }
  }, []);

  useEffect(() => {
    if (props.active) void load();
  }, [props.active, load]);

  return (
    <>
      <div id="coach-toolbar">
        <span id="coach-counts">
          {rows == null ? '' : `${pending} pending · ${total} judged`}
        </span>
        <button type="button" id="coach-refresh" onClick={() => void load()}>
          Refresh
        </button>
      </div>
      <div id="coach-body">
        {err ? <p className="ai-err">{err}</p> : null}
        {rows != null && rows.length === 0 ? (
          <p className="ai-empty">No coach judgments yet.</p>
        ) : null}
        {rows?.map((j) => {
          const title = (j.name || j.observation || j.fingerprint).trim();
          const obs = (j.observation || '').trim();
          const showObs = obs !== '' && obs !== title;
          const meta = [when(j.delivered_at), j.mode === 'retrospective' ? 'retrospective' : '', j.evidence || '']
            .filter(Boolean)
            .join(' · ');
          return (
            <article key={j.fingerprint} className="coach-row">
              <div className="coach-row-head">
                <span className="coach-title" title={title}>{title}</span>
                {j.severity ? (
                  <span className={'coach-sev' + (j.severity === 'high' ? ' coach-sev-high' : '')}>{j.severity}</span>
                ) : null}
                <span className={chip(j.disposition)}>{j.disposition || 'pending'}</span>
              </div>
              {showObs ? <div className="coach-obs">{obs}</div> : null}
              {meta ? <div className="coach-meta">{meta}</div> : null}
              {j.reason ? <div className="coach-detail">{j.reason}</div> : null}
              {j.target_id ? <div className="coach-detail">{j.target_id}</div> : null}
            </article>
          );
        })}
      </div>
    </>
  );
}
