// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';

// The workers strip reads GET /api/workers. "NONE YET" is only honest when
// that list is empty; finished jwork runs still belong on the pane.

type Worker = {
  id: string;
  task?: string;
  status?: string;
  started_at?: string;
  outcome?: string;
};

export function workersLiveLabel(rows: Worker[] | null): string {
  if (rows == null) return '';
  if (rows.length === 0) return 'NONE YET';
  const live = rows.filter((w) => w.status === 'running').length;
  if (live === 1) return '1 live';
  if (live > 1) return `${live} live`;
  return 'none live';
}

function firstLine(text: string | undefined, max: number): string {
  // A finish often opens a code fence on the same line as the sentence
  // ("verbatim.```"). The strip is a preview, so skip fence-only lines and
  // drop a trailing fence; inline `code` stays. A long line that runs the
  // next sentence on ("HEAD.Pulling") stops at that period.
  let line = '';
  for (const raw of (text || '').split('\n')) {
    const trimmed = raw.trim();
    if (!trimmed || /^```[a-zA-Z0-9_-]*$/.test(trimmed)) continue;
    const prose = trimmed.replace(/```+$/, '').trim();
    if (!prose) continue;
    line = prose;
    break;
  }
  if (line.length <= max) return line;
  const window = line.slice(0, max);
  let sentenceEnd = -1;
  for (let i = 0; i < window.length - 1; i++) {
    const ch = window[i];
    if ((ch === '.' || ch === '!' || ch === '?') && /[A-Z]/.test(window[i + 1])) sentenceEnd = i;
  }
  if (sentenceEnd >= 40) return window.slice(0, sentenceEnd + 1);
  return window.slice(0, max - 1) + '…';
}

function when(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

export function WorkersList() {
  const [rows, setRows] = useState<Worker[] | null>(null);

  const load = useCallback(async () => {
    try {
      const r = await fetch('/api/workers');
      if (!r.ok) return;
      const body = (await r.json()) as Worker[];
      setRows(Array.isArray(body) ? body : []);
    } catch {
      /* keep the last list; a blip should not blank the pane */
    }
  }, []);

  useEffect(() => {
    void load();
    const t = window.setInterval(() => void load(), 15000);
    return () => window.clearInterval(t);
  }, [load]);

  return (
    <>
      <div id="workers-header">
        Workers <span id="workers-live">{workersLiveLabel(rows) || 'NONE YET'}</span>
      </div>
      <div id="workers" title="jwork workers">
        {rows?.map((w) => {
          const status = w.status || 'running';
          const task = firstLine(w.task, 160);
          const outcome = firstLine(w.outcome, 160);
          return (
            <div key={w.id} className="worker-row">
              <div className="wr-head">
                <span className="wr-id">{w.id}</span>
                <span className={'wr-status ' + status}>{status}</span>
              </div>
              {task ? <div className="wr-task">{task}</div> : null}
              {when(w.started_at) ? <div className="wr-meta">{when(w.started_at)}</div> : null}
              {outcome && outcome !== task ? <div className="wr-out">{outcome}</div> : null}
            </div>
          );
        })}
      </div>
    </>
  );
}
