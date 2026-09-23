// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';

// 🎯T270: Closed opens the durable aside archive. The button used to do nothing.

type ClosedAside = {
  id: string;
  title?: string;
  kind?: string;
  kind_label?: string;
  closed_at?: string;
};

type Payload = { asides?: ClosedAside[] };

function when(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

export function AsideHistoryPanel(props: { open: boolean; onClose: () => void }) {
  const [rows, setRows] = useState<ClosedAside[] | null>(null);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    try {
      const r = await fetch('/api/asides/history');
      if (!r.ok) throw new Error('closed asides unavailable');
      const body = (await r.json()) as Payload;
      setRows(body.asides || []);
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'closed asides unavailable');
    }
  }, []);

  useEffect(() => {
    if (props.open) void load();
  }, [props.open, load]);

  if (!props.open) return null;

  return (
    <div id="aside-history-panel" className="open" role="region" aria-label="Closed asides history">
      <div className="aside-history-head">
        <span className="ah-hist-label">Closed asides</span>
        <button type="button" id="aside-history-close" onClick={props.onClose} aria-label="Close closed asides">
          ×
        </button>
      </div>
      <div id="aside-history-body" className="aside-history-body">
        {err ? <p className="aside-history-empty">{err}</p> : null}
        {rows != null && rows.length === 0 ? <p className="aside-history-empty">No closed asides.</p> : null}
        {rows && rows.length > 0 ? (
          <ul className="aside-history-list">
            {rows.map((a) => (
              <li key={a.id} className={'aside-history-row aside-hist-' + (a.kind || 'side')}>
                <span className="aside-history-kind">{a.kind_label || a.kind || 'side'}</span>
                <span className="aside-history-title" title={a.title || a.id}>{a.title || a.id}</span>
                {when(a.closed_at) ? <span className="wr-meta">{when(a.closed_at)}</span> : null}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </div>
  );
}
