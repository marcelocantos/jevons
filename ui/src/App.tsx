// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
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
import { PlanUsageBar } from './components/PlanUsageBar';
import { applyTheme, persistTheme, readThemePref, type ThemePref } from './theme';
import { clampRhsWidth, persistRhsWidth, readRhsWidth } from './layout/rhsWidth';
import { useCockpitKeys } from './keys/useCockpitKeys';

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
    raw.tab === 'transcript' || raw.tab === 'coach' ? raw.tab : 'frontier';
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
  useCockpitKeys({ sidebarComposerVisible: tab === 'transcript' });
  const navigate = useNavigate({ from: indexRoute.fullPath });
  const agentsQ = useQuery({
    queryKey: ['agents'],
    queryFn: async () => {
      const r = await fetch('/api/agents');
      if (!r.ok) return [] as AgentRow[];
      const data = await r.json();
      const list = Array.isArray(data) ? data : [];
      return list
        .map((a: { name?: string; purpose?: string; parent?: string; status?: string }) => ({
          name: a.name || '',
          purpose: a.purpose,
          parent: a.parent,
          status: a.status,
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
  const [theme, setTheme] = useState<ThemePref>('system');
  const [rhsW, setRhsW] = useState(360);
  const rhsWRef = useRef(360);
  const dragRef = useRef<{ startX: number; startW: number } | null>(null);
  rhsWRef.current = rhsW;

  useEffect(() => {
    const pref = readThemePref();
    setTheme(pref);
    applyTheme(pref);
    setRhsW(readRhsWidth(window.innerWidth));
  }, []);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      const d = dragRef.current;
      if (!d) return;
      const next = clampRhsWidth(d.startW - (e.clientX - d.startX), window.innerWidth);
      setRhsW(next);
    };
    const onUp = () => {
      if (!dragRef.current) return;
      persistRhsWidth(rhsWRef.current);
      dragRef.current = null;
      document.body.classList.remove('rhs-resizing');
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, []);

  return (
    <div className="cockpit" style={{ ['--rhs-width' as string]: rhsW + 'px' }}>
      <header className="cockpit-bar">
        <span>Jevons</span>
        <PlanUsageBar />
        <div id="theme-toggle" className="theme-toggle">
          {(['system', 'light', 'dark'] as ThemePref[]).map((p) => (
            <button
              key={p}
              type="button"
              data-t={p}
              className={theme === p ? 'active' : ''}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                persistTheme(p);
                setTheme(p);
              }}
            >
              {p}
            </button>
          ))}
        </div>
      </header>
      <div className="cockpit-body">
        <AgentInteraction mux={mux} name="jevons" title="Root" />
        <div
          className="rhs-width-handle"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize sidebar width"
          tabIndex={0}
          onMouseDown={(e) => {
            dragRef.current = { startX: e.clientX, startW: rhsW };
            document.body.classList.add('rhs-resizing');
          }}
        />
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
            {tab === 'frontier' ? (
              <FrontierTable rows={frontierQ.data || []} />
            ) : tab === 'transcript' ? (
              <AgentInteraction mux={mux} name={agent} title={agent} />
            ) : (
              <p className="muted">Coach judgments port next.</p>
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
