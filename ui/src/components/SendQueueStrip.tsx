// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { QueueItem } from '../composer/sendQueue';

/**
 * Queued follow-ups above the composer (🎯T657 slice 2a; 🎯T113 parity).
 * Append = bottom of the queue, and the display puts the next-to-send item
 * at the bottom, nearest the composer — so the list paints newest first.
 */
export function SendQueueStrip(props: {
  items: QueueItem[];
  focusedId?: string | null;
  onSteer: (id: string) => void;
  onInterrupt: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  const items = props.items.slice().reverse();
  return (
    <div id="send-queue" className={items.length ? 'visible' : undefined} aria-label="Queued follow-ups" role="list">
      {items.map((it, i) => {
        const next = i === items.length - 1;
        const focused = props.focusedId === it.id;
        return (
          <div
            key={it.id}
            className={'send-queue-item' + (focused ? ' focused' : '')}
            role="listitem"
            data-queue-id={it.id}
            data-queue-next={next ? 'true' : undefined}
            aria-current={focused ? 'true' : undefined}
          >
            <span className="sq-text" title={it.text}>{it.text}</span>
            <span className="sq-actions">
              <button type="button" className="sq-send-now" title="Fold into the running turn (⌘Enter)" onClick={() => props.onSteer(it.id)}>
                Steer
              </button>
              <button type="button" className="sq-interrupt" title="Interrupt the turn and send this (⌘⇧Enter)" onClick={() => props.onInterrupt(it.id)}>
                Cut in
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
