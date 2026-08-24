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
import { TargetHoverCard } from './TargetHoverCard';

export type { FrontierRow };

export function FrontierTable(props: { rows: FrontierRow[] }) {
  const [hover, setHover] = useState<FrontierRow | null>(null);
  const cacheRef = useRef<HoverCardCache>({});
  useEffect(() => {
    expireCardCache(cacheRef.current, props.rows);
  }, [props.rows]);
  const card = hover ? hoverCardMarkdown(cacheRef.current, hover) : '';
  return (
    <>
      <table id="frontier-table" aria-label="Bullseye frontier">
        <tbody>
          {props.rows.map((r) => {
            const fan = formatFanout(r.fanout, r.id, r.dependents);
            return (
              <tr
                key={r.id}
                onMouseEnter={() => setHover(r)}
                onMouseLeave={() => setHover(null)}
              >
                <td className="ft-id has-instant-tip">{'🎯' + r.id}</td>
                <td className="ft-name has-instant-tip">{shortName(r.name, 72)}</td>
                <td className="ft-status has-instant-tip">{formatStatus(r.status)}</td>
                <td className={fan.visible ? 'ft-fanout has-instant-tip' : 'ft-fanout ft-fanout-empty'}>
                  {fan.text}
                </td>
                <td className="ft-play">
                  <button type="button" className="ft-play-btn" aria-label="start">{'\u25B6'}</button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <TargetHoverCard markdown={card} visible={!!hover} />
    </>
  );
}
