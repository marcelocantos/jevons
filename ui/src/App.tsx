// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import {
  Outlet,
  RouterProvider,
  createRootRoute,
  createRoute,
  createRouter,
  useNavigate,
} from '@tanstack/react-router';
import { MuxClient, muxUrl } from './mux/client';
import { AgentInteraction } from './components/AgentInteraction';
import { AgentTree, type AgentRow } from './components/AgentTree';
import { SidebarPanel, type SidebarTab } from './components/SidebarPanel';
import { FrontierTable, type FrontierRow } from './components/FrontierTable';

const queryClient = new QueryClient();

let muxSingleton: MuxClient | null = null;
function getMux(): MuxClient {
  if (!muxSingleton) {
    muxSingleton = new MuxClient(muxUrl());
    muxSingleton.connect();
  }
  return muxSingleton;
}

type Search = { agent: string; tab: SidebarTab };

function parseSearch(raw: Record<string, unknown>): Search {
  const agent =
    typeof raw.agent === 'string' && raw.agent.trim() ? raw.agent.trim() : 'jevons-po';
  const tab: SidebarTab =
    raw.tab === 'frontier' || raw.tab === 'more' ? raw.tab : 'transcript';
  return { agent, tab };
}

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  validateSearch: parseSearch,
  component: Cockpit,
});

const routeTree = rootRoute.addChildren([indexRoute]);
const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

function Cockpit() {
  const mux = getMux();
  const { agent, tab } = indexRoute.useSearch();
  const navigate = useNavigate({ from: indexRoute.fullPath });
  const agentsQ = useQuery({
    queryKey: ['agents'],
    queryFn: async () => {
      const r = await fetch('/api/agents');
      if (!r.ok) return [] as AgentRow[];
      const data = await r.json();
      const list = Array.isArray(data) ? data : [];
      return list
        .map((a: { name?: string; purpose?: string }) => ({
          name: a.name || '',
          purpose: a.purpose,
        }))
        .filter((a: AgentRow) => a.name);
    },
    refetchInterval: 5000,
  });
  const frontierQ = useQuery({
    queryKey: ['frontier'],
    queryFn: async () => {
      const r = await fetch('/api/frontier');
      if (!r.ok) return [] as FrontierRow[];
      const data = await r.json();
      const targets = Array.isArray(data?.targets) ? data.targets : [];
      return targets.map((t: { id?: string; name?: string; status?: string }) => ({
        id: t.id || '',
        name: t.name || '',
        status: t.status,
      }));
    },
    enabled: tab === 'frontier',
    refetchInterval: 8000,
  });
  const agents =
    agentsQ.data && agentsQ.data.length ? agentsQ.data : [{ name: 'jevons' }, { name: 'jevons-po' }];

  return (
    <div className="cockpit">
      <header className="cockpit-bar">Jevons</header>
      <div className="cockpit-body">
        <AgentInteraction mux={mux} name="jevons" title="Root" />
        <aside className="cockpit-rhs">
          <AgentTree
            agents={agents}
            selected={agent}
            onSelect={(name) => navigate({ search: { agent: name, tab } })}
          />
          <SidebarPanel
            tab={tab}
            onTab={(next) => navigate({ search: { agent, tab: next } })}
          >
            {tab === 'transcript' ? (
              <AgentInteraction mux={mux} name={agent} title={agent} />
            ) : tab === 'frontier' ? (
              <FrontierTable rows={frontierQ.data || []} />
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
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
