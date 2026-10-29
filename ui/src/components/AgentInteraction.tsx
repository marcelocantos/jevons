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
import type { QueueItem } from '../composer/sendQueue';
import type { DeliveryMode } from '../composer/deliveryMode';
import { phaseAtRest, phaseSampleFromUnknown } from '../conversation/overseerPhase';
import { useSendQueue } from '../composer/useSendQueue';
import { imageFromId, splitImageMarkers } from '../composer/images';
import { planExplicitAside, postCreateAside } from '../composer/openAside';
import { usePendingImages } from '../store/pendingImages';
import { reconcileQueueFocus } from '../composer/queueFocus';
import { SendQueueStrip } from './SendQueueStrip';
import { EscalationStrip } from './EscalationStrip';

/** Both browser-held and daemon-held owner turns share the same queue strip. */
export function queueItemsFromFrames(frames: unknown[], client: QueueItem[]): QueueItem[] {
  const held = displayRows(frames).filter((row) => row.kind === 'user' && row.origin === 'owner' && row.delivery?.state === 'held' && row.msgId)
    .map((row) => {
      const frame = frames.find((f) => f && typeof f === 'object' && (f as { msg_id?: string }).msg_id === row.msgId && (f as { type?: string }).type === 'send_error' && (f as { state?: string }).state === 'held') as { when?: number; timestamp?: number } | undefined;
      const when = row.when ?? frame?.when ?? frame?.timestamp;
      return { id: row.msgId!, text: row.text, queuedAt: when && when < 1e11 ? when * 1000 : when ?? 0, daemonHeld: true };
    });
  return [...held, ...client].sort((a, b) => (a.queuedAt ?? 0) - (b.queuedAt ?? 0));
}

export function AgentInteraction(props: {
  mux: MuxClient | null;
  name: string;
  title?: string;
  density?: Density;
  paneActive?: boolean;
  connected?: boolean;
  /** Vendor stop, such as a spent plan. The composer stays closed until it clears. */
  planWall?: string;
  /** 🎯T966: fleet-wide owner pause — pink composer, held sends. */
  fleetPaused?: boolean;
  /**
   * 🎯T899 / 🎯T903: a busy seat takes a plain Enter at once and the daemon
   * escalates it (steer, then interrupt). False holds it in the pane's queue
   * until idle instead. Default true for every pane, the overseer included.
   */
  escalate?: boolean;
  onMeta?: (meta: ConversationMeta | null) => void;
  /** 🎯T1122: this pane is the owner's main chat with the overseer. */
  ownerChat?: boolean;
  /**
   * 🎯T247 / 🎯T251: main composer opened an explicit aside (target:/aside:/…).
   * App selects that row on the Transcript tab so #agent-inspect-input appears.
   */
  onOpenAside?: (id: string) => void;
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
  const busy = !!phase && !phaseAtRest(phase);
  const connected = props.connected ?? true;
  const queue = useSendQueue(props.name, {
    busy,
    wireOpen: connected,
    sendNow: (text, mode) => conv.send(text, { mode }),
    // 🎯T899 / 🎯T903: a message to a busy agent — the overseer included —
    // goes to the daemon at once, which steers it into the turn and
    // interrupts later if it is not taken.
    escalate: props.escalate ?? true,
  });
  // 🎯T657 slice 2b: Alt+↑/↓ focus over the queue; a drained or removed
  // item drops the focus rather than pointing at nothing.
  const [queueFocusRaw, setQueueFocus] = useState<string | null>(null);
  const allQueueItems = useMemo(() => queueItemsFromFrames(conv.frames, queue.items), [conv.frames, queue.items]);
  const queueFocus = reconcileQueueFocus(queueFocusRaw, allQueueItems);
  const queueAction = (id: string, mode: DeliveryMode) => {
    if (props.planWall || props.fleetPaused) return;
    if (allQueueItems.find((item) => item.id === id)?.daemonHeld) {
      if (mode === 'interrupt') conv.interruptHeld(id);
      else conv.steerHeld(id);
    } else queue.sendItem(id, mode);
  };
  const removeQueued = (id: string) => {
    if (allQueueItems.find((item) => item.id === id)?.daemonHeld) conv.removeHeld(id);
    else queue.remove(id);
  };
  useEffect(() => {
    setQueueFocus(null);
  }, [props.name]);
  // 🎯T562.1: Edit returns a queued item to the composer (above any draft
  // already there) and takes it out of the queue; Enter queues it again.
  const setDraft = useDrafts((s) => s.setDraft);
  const editQueued = (id: string) => {
    const item = allQueueItems.find((it) => it.id === id);
    if (!item) return;
    const current = useDrafts.getState().drafts[props.name] || '';
    removeQueued(id);
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
        ownerChat={props.ownerChat}
        frames={conv.frames}
        provisionalByStream={conv.provisionalByStream}
        meta={conv.meta}
        ready={conv.ready}
        following={following}
        onPageOlder={() => conv.pageOlder(props.name === 'jevons' ? 250 : 50)}
        olderPage={conv.olderPage}
        onRetryOlder={conv.retryOlder}
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
        items={allQueueItems}
        focusedId={queueFocus}
        onSteer={(id) => queueAction(id, 'steer')}
        onInterrupt={(id) => queueAction(id, 'interrupt')}
        onRemove={removeQueued}
        onEdit={editQueued}
      />
      {comfortable ? <OverseerPhaseStrip connected={connected} meta={conv.meta} /> : null}
      <UserRequest
        name={props.name}
        density={density}
        hold={props.fleetPaused ? 'Paused...' : props.planWall}
        fleetPaused={props.fleetPaused}
        disabled={props.planWall || props.fleetPaused ? true : undefined}
        onSend={(t, opts) => {
          if (props.planWall || props.fleetPaused) return;
          // 🎯T95 / 🎯T247 / 🎯T250: target:/aside:/capture:/idea: open an
          // aside immediately. They must not paint as a main .msg.user.
          if (comfortable) {
            const plan = planExplicitAside(t);
            if (plan) {
              props.onOpenAside?.(plan.id);
              void postCreateAside(plan);
              return { queued: true };
            }
          }
          const result = queue.submit(t, opts?.mode ?? 'submit');
          // 🎯T1126: a send that actually goes out now (not merely queued
          // for later) is the owner re-attaching to the live end — restore
          // follow so the sent bubble and the reply paint in view instead
          // of leaving a ↓ Latest detour behind.
          if (!result.queued) {
            conv.rejoinLive();
            setFollowing(true);
            setFollowEpoch((n) => n + 1);
          }
          return result;
        }}
        onInterrupt={props.planWall || props.fleetPaused ? undefined : () => conv.send('', { mode: 'interrupt' })}
        queue={{ items: allQueueItems, focusedId: queueFocus, onFocus: setQueueFocus, onSend: queueAction }}
        history={history}
        onRecall={setRecalled}
      />
    </div>
  );
}
