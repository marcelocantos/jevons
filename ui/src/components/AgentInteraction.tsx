// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useMemo, useState } from 'react';
import { MuxClient } from '../mux/client';
import { useConversation, type ConversationMeta } from '../conversation/useConversation';
import { normalizeDensity, type Density } from '../density';
import { AgentTranscript } from './AgentTranscript';
import { OverseerPhaseStrip } from './OverseerPhaseStrip';
import { UserRequest, type RecalledRequest } from './UserRequest';
import { displayRows } from '../conversation/display';

export function AgentInteraction(props: {
  mux: MuxClient | null;
  name: string;
  title?: string;
  density?: Density;
  paneActive?: boolean;
  connected?: boolean;
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
  return (
    <div
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
          <div id="send-queue" aria-label="Queued follow-ups" role="list" />
          <OverseerPhaseStrip connected={props.connected ?? true} meta={conv.meta} />
        </>
      ) : null}
      <UserRequest
        name={props.name}
        density={density}
        onSend={(t) => conv.send(t)}
        history={history}
        onRecall={setRecalled}
      />
    </div>
  );
}
