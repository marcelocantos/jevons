// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { MuxClient } from '../mux/client';
import { useConversation } from '../conversation/useConversation';
import { normalizeDensity, type Density } from '../density';
import { AgentTranscript } from './AgentTranscript';
import { UserRequest } from './UserRequest';
import { pixelFixtureActive } from '../visual/oldCockpitFixture';

export function AgentInteraction(props: {
  mux: MuxClient | null;
  name: string;
  title?: string;
  density?: Density;
}) {
  const density = normalizeDensity(props.density);
  const conv = useConversation(props.mux, props.name);
  const comfortable = density === 'comfortable';
  return (
    <div
      id={comfortable ? 'chat-pane' : 'agent-inspect'}
      className={
        (comfortable ? 'conversation-widget density-' + density : 'rhs-tab-pane conversation-widget density-' + density)
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
        onPageOlder={conv.meta?.older ? () => conv.pageOlder(50) : undefined}
        onLeaveLive={conv.leaveLive}
      />
      {comfortable ? (
        <>
          <button type="button" id="jump-bottom" hidden title="Jump to latest (End / ⌘↓)">
            ↓ Latest
          </button>
          <div id="attention-bar" aria-label="Attention asides" hidden>
            <div id="attention-stack" role="list" />
            <div id="attention-actions" aria-label="Attention aside actions" />
          </div>
          <div id="send-queue" aria-label="Queued follow-ups" role="list" />
        </>
      ) : null}
      {conv.error ? <div className="ai-err">{conv.error}</div> : null}
      <UserRequest
        name={props.name}
        density={density}
        disabled={pixelFixtureActive() ? false : undefined}
        onSend={(t) => conv.send(t)}
      />
    </div>
  );
}
