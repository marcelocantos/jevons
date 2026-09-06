// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';
import {
  API_PATH,
  POLL_MS,
  countsText,
  detailText,
  emptyText,
  normalizePayload,
  type CoachModel,
} from '../coach/dispositions';

/** RHS Coach tab (🎯T354): durable dispositions list, not MCP chat dumps. */

export function CoachPane(props: { active: boolean }) {
  const [model, setModel] = useState<CoachModel | null>(null);

  const load = useCallback(async (quiet: boolean) => {
    try {
      const r = await fetch(API_PATH);
      if (!r.ok) throw new Error('HTTP ' + r.status);
      const payload: unknown = await r.json();
      setModel(normalizePayload(payload));
    } catch (err) {
      if (quiet) return;
      setModel(normalizePayload(null, err));
    }
  }, []);

  useEffect(() => {
    if (!props.active) return;
    void load(false);
    const id = window.setInterval(() => {
      void load(true);
    }, POLL_MS);
    return () => window.clearInterval(id);
  }, [props.active, load]);

  const counts = countsText(model);
  return (
    <>
      <div id="coach-toolbar">
        <span id="coach-counts" title="Judgments delivered / still awaiting a disposition">
          {counts}
        </span>
        <button type="button" id="coach-refresh" title="Reload the coach disposition ledger" onClick={() => void load(false)}>
          Refresh
        </button>
      </div>
      <div id="coach-body">
        {!model ? (
          <p className="ai-empty">Loading…</p>
        ) : model.error ? (
          <div className="ai-err">Coach ledger unavailable: {model.error}</div>
        ) : model.empty ? (
          <div className="ai-empty">{emptyText()}</div>
        ) : (
          model.rows.map((r) => {
            const detail = detailText(r);
            const meta: string[] = [];
            if (r.deliveredAt) meta.push(new Date(r.deliveredAt).toLocaleString());
            if (r.evidence) meta.push(r.evidence);
            return (
              <div key={r.fingerprint || r.title} className="coach-row" title={r.fingerprint}>
                <div className="coach-row-head">
                  <span className="coach-title">
                    {r.retro ? '⏮ ' : ''}
                    {r.title}
                  </span>
                  {r.severity ? (
                    <span className={'coach-sev' + (r.severity === 'high' ? ' coach-sev-high' : '')}>{r.severity}</span>
                  ) : null}
                  <span className={'coach-chip' + (r.pending ? ' coach-pending' : '')}>{r.dispositionLabel}</span>
                </div>
                {r.observation && r.observation !== r.title ? <div className="coach-obs">{r.observation}</div> : null}
                {detail ? <div className="coach-detail">{detail}</div> : null}
                {meta.length ? <div className="coach-meta">{meta.join(' · ')}</div> : null}
              </div>
            );
          })
        )}
      </div>
    </>
  );
}
