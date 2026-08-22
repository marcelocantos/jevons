// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useMemo, useState } from 'react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { MuxClient, muxUrl } from './mux/client';
import { AgentInteraction } from './components/AgentInteraction';
import { AgentTree, type AgentRow } from './components/AgentTree';
import { SidebarPanel, type SidebarTab } from './components/SidebarPanel';
import { FrontierTable } from './components/FrontierTable';

const queryClient = new QueryClient();

function Cockpit() {
  const mux = useMemo(() => {
    const c = new MuxClient(muxUrl());
    c.connect();
    return c;
  }, []);
  const [selected, setSelected] = useState('jevons-po');
  const [tab, setTab] = useState<SidebarTab>('transcript');
  const agentsQ = useQuery({
    queryKey: ['agents'],
    queryFn: async () => {
      const r = await fetch('/api/agents');
      if (!r.ok) return [] as AgentRow[];
      const data = await r.json();
      const list = Array.isArray(data) ? data : [];
      return list.map((a: { name?: string; Name?: string; purpose?: string }) => ({
        name: a.name || a.Name || '',
        purpose: a.purpose,
      })).filter((a: AgentRow) => a.name);
    },
    refetchInterval: 5000,
  });
  const agents = agentsQ.data && agentsQ.data.length ? agentsQ.data : [{ name: 'jevons' }, { name: 'jevons-po' }];

  return (
    <div className="cockpit">
      <header className="cockpit-bar">Jevons</header>
      <div className="cockpit-body">
        <AgentInteraction mux={mux} name="jevons" title="Root" />
        <aside className="cockpit-rhs">
          <AgentTree agents={agents} selected={selected} onSelect={setSelected} />
          <SidebarPanel tab={tab} onTab={setTab}>
            {tab === 'transcript' ? (
              <AgentInteraction mux={mux} name={selected} title={selected} />
            ) : tab === 'frontier' ? (
              <FrontierTable rows={[]} />
            ) : (
              <p className="muted">More chrome ports next.</p>
            )}
          </SidebarPanel>
        </aside>
      </div>
    </div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Cockpit />
    </QueryClientProvider>
  );
}
