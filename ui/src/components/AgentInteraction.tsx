// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { MuxClient } from '../mux/client';
import { useConversation } from '../conversation/useConversation';
import { AgentTranscript } from './AgentTranscript';
import { UserRequest } from './UserRequest';

export function AgentInteraction(props: { mux: MuxClient | null; name: string; title?: string }) {
  const conv = useConversation(props.mux, props.name);
  return (
    <section className="agent-interaction" data-agent={props.name}>
      <header className="agent-interaction-head">{props.title || props.name}</header>
      <AgentTranscript
        name={props.name}
        frames={conv.frames}
        meta={conv.meta}
        ready={conv.ready}
        onPageOlder={
          conv.meta?.older
            ? () => conv.page(conv.meta!.start ?? conv.meta!.older ?? 0, 200)
            : undefined
        }
      />
      {conv.error ? <div className="agent-error">{conv.error}</div> : null}
      <UserRequest name={props.name} onSend={(t) => conv.send(t)} />
    </section>
  );
}
