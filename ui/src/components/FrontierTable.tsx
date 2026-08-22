// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useState } from 'react';
import { TargetHoverCard } from './TargetHoverCard';

export type FrontierRow = { id: string; name: string; status?: string };

export function FrontierTable(props: { rows: FrontierRow[] }) {
  const [hover, setHover] = useState<FrontierRow | null>(null);
  return (
    <div className="frontier-wrap">
      <table className="frontier-table">
        <thead>
          <tr>
            <th>id</th>
            <th>name</th>
          </tr>
        </thead>
        <tbody>
          {props.rows.map((r) => (
            <tr
              key={r.id}
              onMouseEnter={() => setHover(r)}
              onMouseLeave={() => setHover(null)}
            >
              <td>{r.id}</td>
              <td>{r.name}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <TargetHoverCard
        id={hover?.id || ''}
        name={hover?.name || ''}
        visible={!!hover}
      />
    </div>
  );
}
