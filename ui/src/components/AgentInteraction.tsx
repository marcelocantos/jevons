// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useRef, useState } from 'react';
import { useDrafts } from '../store/drafts';
import { isEffectivelyEmpty } from '../composer/wispr';
import { MuxClient } from '../mux/client';
import { useConversation, type ConversationMeta } from '../conversation/useConversation';
import { normalizeDensity, type Density } from '../density';
import { AgentTranscript } from './AgentTranscript';
import { OverseerPhaseStrip } from './OverseerPhaseStrip';
import { UserRequest, type RecalledRequest } from './UserRequest';
import { displayRows } from '../conversation/display';
import { PHASE_IDLE, phaseSampleFromUnknown } from '../conversation/overseerPhase';
import { useSendQueue } from '../composer/useSendQueue';
import { imageFromId, splitImageMarkers } from '../composer/images';
import { usePendingImages } from '../store/pendingImages';
import { reconcileQueueFocus } from '../composer/queueFocus';
import { SendQueueStrip } from './SendQueueStrip';
import { EscalationStrip } from './EscalationStrip';

export function AgentInteraction(props: {
  mux: MuxClient | null;
  name: string;
  title?: string;
  density?: Density;
  paneActive?: boolean;
  connected?: boolean;
  /** Vendor stop, such as a spent plan. The composer stays closed until it clears. */
  planWall?: string;
  onMeta?: (meta: ConversationMeta | null) => void;
}) {
  const density = normalizeDensity(props.density);
  const conv = useConversation(props.mux, props.name);
  const [following, setFollowing] = useState(true);
  const [followEpoch, setFollowEpoch] = useState(0);
  const [recalled, setRecalled] = useState<RecalledRequest | null>(null);
  const history = useMemo(() => displayRows(conv.frames, { inspect: density === 'compact' })
    .filter((row) => row.kind === 'user' && row.origin === 'owner' && !!row.id)
    .map((row) => ({ id: row.id!, text: row.text })), [conv.frames, density]);
  useEffect(() => {
    props.onMeta?.(conv.meta);
  }, [conv.meta, props.onMeta]);
  useEffect(() => {
    setFollowing(true);
    setRecalled(null);
  }, [props.name]);
  const comfortable = density === 'comfortable';
  const rootRef = useRef<HTMLDivElement>(null);
  // 🎯T657: the seat is busy when its painted phase is anything but idle.
  // Seats without a phase sample (fleet transcripts) send straight through
  // and the daemon queues on busy, as before.
  const phase = phaseSampleFromUnknown(conv.meta);
  const busy = !!phase && phase.phase !== PHASE_IDLE;
  const connected = props.connected ?? true;
  const queue = useSendQueue(props.name, {
    busy,
    wireOpen: connected,
    sendNow: (text, mode) => conv.send(text, { mode }),
    // 🎯T899/🎯T903: a message to a busy agent goes to the daemon at once,
    // which steers it into the turn and interrupts later if it is not
    // taken. The overseer pane escalates identically to any other agent
    // pane (T899 originally excluded it, leaving an owner message to a busy
    // overseer waiting in the client-side queue for minutes); the strip and
    // Steer/Cut in controls are unconditional already.
    escalate: true,
  });
  // 🎯T657 slice 2b: Alt+↑/↓ focus over the queue; a drained or removed
  // item drops the focus rather than pointing at nothing.
  const [queueFocusRaw, setQueueFocus] = useState<string | null>(null);
  const queueFocus = reconcileQueueFocus(queueFocusRaw, queue.items);
  useEffect(() => {
    setQueueFocus(null);
  }, [props.name]);
  // 🎯T562.1: Edit returns a queued item to the composer (above any draft
  // already there) and takes it out of the queue; Enter queues it again.
  const setDraft = useDrafts((s) => s.setDraft);
  const editQueued = (id: string) => {
    const item = queue.items.find((it) => it.id === id);
    if (!item) return;
    const current = useDrafts.getState().drafts[props.name] || '';
    queue.remove(id);
    // Images go back to the composer as chips, not as marker text.
    const { ids, text } = splitImageMarkers(item.text);
    if (ids.length) usePendingImages.getState().add(props.name, ids.map(imageFromId));
    setDraft(props.name, isEffectivelyEmpty(current) ? text : `${text}\n${current}`);
    setRecalled(null);
    queueMicrotask(() => rootRef.current?.querySelector('textarea')?.focus());
  };
  return (
    <div
      ref={rootRef}
      id={comfortable ? 'chat-pane' : 'agent-inspect'}
      className={
        comfortable
          ? 'conversation-widget density-' + density
          : 'rhs-tab-pane conversation-widget density-' + density + (props.paneActive ? ' active' : '')
      }
      data-density={density}
      data-agent-id={props.name}
    >
      {comfortable ? null : (
        <div id="agent-inspect-header">
          <span className="ai-label">Transcript</span>
          <span className="ai-name" id="agent-inspect-name">
            {props.title || props.name}
          </span>
        </div>
      )}
      <AgentTranscript
        name={props.name}
        density={density}
        frames={conv.frames}
        meta={conv.meta}
        ready={conv.ready}
        onPageOlder={() => conv.pageOlder(50)}
        onLeaveLive={conv.leaveLive}
        followEpoch={followEpoch}
        onFollowChange={setFollowing}
        recalledId={recalled?.id}
        onResend={conv.resend}
      />
      {comfortable ? (
        <>
          <button
            type="button"
            id="jump-bottom"
            hidden={following}
            title="Jump to latest (End / ⌘↓)"
            onClick={() => {
              conv.rejoinLive();
              setFollowing(true);
              setFollowEpoch((n) => n + 1);
            }}
          >
            ↓ Latest
          </button>
          <div id="attention-bar" aria-label="Attention asides" hidden>
            <div id="attention-stack" role="list" />
            <div id="attention-actions" aria-label="Attention aside actions" />
          </div>
        </>
      ) : null}
      <EscalationStrip notice={conv.escalation} agent={props.name} onDismiss={conv.dismissEscalation} />
      <SendQueueStrip
        id={comfortable ? 'send-queue' : 'agent-inspect-send-queue'}
        items={queue.items}
        focusedId={queueFocus}
        onSteer={(id) => { if (!props.planWall) queue.sendItem(id, 'steer'); }}
        onInterrupt={(id) => { if (!props.planWall) queue.sendItem(id, 'interrupt'); }}
        onRemove={queue.remove}
        onEdit={editQueued}
      />
      {comfortable ? <OverseerPhaseStrip connected={connected} meta={conv.meta} /> : null}
      <UserRequest
        name={props.name}
        density={density}
        hold={props.planWall}
        disabled={props.planWall ? true : undefined}
        onSend={(t, opts) => {
          if (props.planWall) return;
          return queue.submit(t, opts?.mode ?? 'submit');
        }}
        onInterrupt={props.planWall ? undefined : () => conv.send('', { mode: 'interrupt' })}
        queue={{ items: queue.items, focusedId: queueFocus, onFocus: setQueueFocus, onSend: queue.sendItem }}
        history={history}
        onRecall={setRecalled}
      />
    </div>
  );
}
