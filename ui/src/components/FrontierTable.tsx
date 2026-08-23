// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { TargetHoverCard } from './TargetHoverCard';

export type FrontierRow = { id: string; name: string; status?: string; fanout?: number };

const STATUS_ABBR: Record<string, string> = {
  identified: 'Id',
  converging: 'Cv',
  achieved: 'Ac',
};

function formatStatus(status?: string): string {
  const key = String(status || '')
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, '_');
  return STATUS_ABBR[key] || 'Id';
}

function shortName(s: string, n: number): string {
  const t = String(s || '');
  if (t.length <= n) return t;
  return t.slice(0, n - 1) + '…';
}

export function FrontierTable(props: { rows: FrontierRow[] }) {
  const [hover, setHover] = useState<FrontierRow | null>(null);
  return (
    <>
      <table id="frontier-table" aria-label="Bullseye frontier">
        <tbody>
          {props.rows.map((r) => (
            <tr
              key={r.id}
              onMouseEnter={() => setHover(r)}
              onMouseLeave={() => setHover(null)}
            >
              <td className="ft-id has-instant-tip">{'🎯' + r.id}</td>
              <td className="ft-name has-instant-tip">{shortName(r.name, 72)}</td>
              <td className="ft-status has-instant-tip">{formatStatus(r.status)}</td>
              <td className={r.fanout ? 'ft-fanout has-instant-tip' : 'ft-fanout ft-fanout-empty'}>
                {r.fanout ? r.fanout + '\u169B' : ''}
              </td>
              <td className="ft-play">
                <button type="button" className="ft-play-btn" aria-label="start">{'\u25B6'}</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <TargetHoverCard
        id={hover?.id || ''}
        name={hover?.name || ''}
        visible={!!hover}
      />
    </>
  );
}
