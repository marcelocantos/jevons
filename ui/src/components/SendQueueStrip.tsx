// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { QueueItem } from '../composer/sendQueue';
import { imageThumbSrc, splitImageMarkers } from '../composer/images';

/**
 * Queued follow-ups above the composer (🎯T657 slice 2a; 🎯T113 parity).
 * Append = bottom of the queue, and the display puts the next-to-send item
 * at the bottom, nearest the composer — so the list paints newest first.
 */
export function SendQueueStrip(props: {
  /** DOM id: main keeps `send-queue`; the sidebar pane needs its own so ids stay unique (🎯T562.1). */
  id?: string;
  items: QueueItem[];
  focusedId?: string | null;
  onSteer: (id: string) => void;
  onInterrupt: (id: string) => void;
  onRemove: (id: string) => void;
  /** Take the item out of the queue and back into the composer to be edited. */
  onEdit: (id: string) => void;
}) {
  const items = props.items.slice().reverse();
  return (
    <div id={props.id ?? 'send-queue'} className={'send-queue' + (items.length ? ' visible' : '')} aria-label="Queued follow-ups" role="list">
      {items.map((it, i) => {
        const next = i === items.length - 1;
        const focused = props.focusedId === it.id;
        const { ids, text } = splitImageMarkers(it.text);
        return (
          <div
            key={it.id}
            className={'send-queue-item' + (focused ? ' focused' : '')}
            role="listitem"
            data-queue-id={it.id}
            data-queue-next={next ? 'true' : undefined}
            aria-current={focused ? 'true' : undefined}
          >
            {ids.map((id, n) => (
              <img key={id + n} className="sq-thumb" src={imageThumbSrc(id)} alt={'queued image ' + id} />
            ))}
            <span className="sq-text" title={text || it.text}>{text}</span>
            <span className="sq-actions">
              <button type="button" className="sq-send-now" title="Fold into the running turn (⌘Enter)" onClick={() => props.onSteer(it.id)}>
                Steer
              </button>
              <button type="button" className="sq-interrupt" title="Interrupt the turn and send this (⌘⇧Enter)" onClick={() => props.onInterrupt(it.id)}>
                Cut in
              </button>
              <button type="button" className="sq-edit" title="Return to the composer to edit; Enter queues it again" onClick={() => props.onEdit(it.id)}>
                Edit
              </button>
              <button type="button" className="sq-remove" title="Remove from the queue" onClick={() => props.onRemove(it.id)}>
                Remove
              </button>
            </span>
          </div>
        );
      })}
    </div>
  );
}
