// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
import {
  expireCardCache,
  formatFanout,
  formatStatus,
  hoverCardMarkdown,
  shortName,
  type FrontierRow,
  type HoverCardCache,
} from '../frontier/table';
import { InstantTip } from './InstantTip';
import { TargetHoverCard } from './TargetHoverCard';

export type { FrontierRow };

function FanCell(props: { row: FrontierRow }) {
  const fan = formatFanout(props.row.fanout, props.row.id, props.row.dependents);
  return <td className={fan.visible ? 'ft-fanout' : 'ft-fanout ft-fanout-empty'}>{fan.text}</td>;
}

function FrontierRowView(props: { row: FrontierRow; cache: HoverCardCache }) {
  const [nameEl, setNameEl] = useState<HTMLTableCellElement | null>(null);
  const md = hoverCardMarkdown(props.cache, props.row);
  return (
    <tr>
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
      <td className="ft-play">
        <button type="button" className="ft-play-btn" aria-label="start">
          {'\u25B6'}
        </button>
      </td>
    </tr>
  );
}

export function FrontierTable(props: { rows: FrontierRow[] }) {
  const cacheRef = useRef<HoverCardCache>({});
  useEffect(() => {
    expireCardCache(cacheRef.current, props.rows);
  }, [props.rows]);
  return (
    <table id="frontier-table" aria-label="Bullseye frontier">
      <tbody>
        {props.rows.map((r) => (
          <FrontierRowView key={r.id} row={r} cache={cacheRef.current} />
        ))}
      </tbody>
    </table>
  );
}
