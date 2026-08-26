// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useRef, useState } from 'react';
import { keepPreviousData, QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import {
  Outlet,
  RouterProvider,
  createRootRoute,
  createRoute,
  createRouter,
  useNavigate,
} from '@tanstack/react-router';
import { MuxClient, muxUrl } from './mux/client';
import { degradedBannerText } from './conversation/degraded';
import type { ConversationMeta } from './conversation/reduce';
import { AgentInteraction } from './components/AgentInteraction';
import { AgentTree, type AgentRow } from './components/AgentTree';
import { SidebarPanel, type SidebarTab } from './components/SidebarPanel';
import { FrontierTable, type FrontierRow } from './components/FrontierTable';
import { FrontierRowsContext } from './frontier/rows';
import { toFrontierRows } from './frontier/table';
import { PlanUsageBar } from './components/PlanUsageBar';
import { MermaidVizPanel } from './components/MermaidVizPanel';
import { applyTheme, persistTheme, readThemePref, type ThemePref } from './theme';
import {
  DEFAULT_FLEET_FRACTION,
  DEFAULT_SIDEBAR_WIDTH,
  fleetFractionFromPointer,
  load as loadRhsLayout,
  save as saveRhsLayout,
  sidebarWidthFromPointer,
  stylesForState,
  type RhsLayoutState,
} from './layout/rhsLayout';
import { mergeAgentChrome } from './plan/modelPrefix';
import { useCockpitKeys } from './keys/useCockpitKeys';
import {
  pixelFixtureActive,
  pixelFixtureAgents,
  pixelFixtureFrontier,
  PIXEL_FIXTURE_READY,
} from './visual/oldCockpitFixture';

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
  const lastAgentsRef = useRef<AgentRow[]>([]);
  const [degraded, setDegraded] = useState('');
  const [graphOpen, setGraphOpen] = useState(false);
  const [graphNonce, setGraphNonce] = useState(0);
  const onJevonsMeta = useCallback((meta: ConversationMeta | null) => {
    setDegraded(degradedBannerText(meta));
  }, []);
  const agentsQ = useQuery({
    queryKey: ['agents'],
    queryFn: async () => {
      const r = await fetch('/api/agents');
      if (!r.ok) return lastAgentsRef.current;
      const data = await r.json();
      const list = Array.isArray(data) ? data : [];
      const rows = list
        .map((a: {
          name?: string;
          purpose?: string;
          parent?: string;
          status?: string;
          running?: boolean;
          phase?: string;
          step?: string;
          progress?: string;
          provider?: string;
          model?: string;
          workdir?: string;
        }) => ({
          name: a.name || '',
          purpose: a.purpose,
          parent: a.parent,
          status: a.status,
          running: a.running,
          phase: a.phase,
          step: a.step,
          progress: a.progress,
          provider: a.provider,
          model: a.model,
          workdir: a.workdir,
        }))
        .filter((a: AgentRow) => a.name);
      const merged = mergeAgentChrome(lastAgentsRef.current, rows);
      lastAgentsRef.current = merged;
      return merged;
    },
    placeholderData: keepPreviousData,
    staleTime: 4_000,
    refetchInterval: 5000,
  });
  const frontierQ = useQuery({
    queryKey: ['frontier'],
    queryFn: async () => {
      const r = await fetch('/api/frontier');
      if (!r.ok) return [] as FrontierRow[];
      return toFrontierRows(await r.json());
    },
    refetchInterval: 8000,
  });
  const fixture = pixelFixtureActive();
  useEffect(() => {
    if (!fixture) return;
    document.documentElement.setAttribute('data-pixel-fixture', '1');
    return () => document.documentElement.removeAttribute('data-pixel-fixture');
  }, [fixture]);
  const agents = fixture
    ? pixelFixtureAgents()
    : agentsQ.data && agentsQ.data.length
      ? agentsQ.data
      : [{ name: 'jevons' }, { name: 'jevons-po' }];
  const frontierRows = fixture ? pixelFixtureFrontier() : frontierQ.data || [];
  const [theme, setTheme] = useState<ThemePref>('system');
  const [layout, setLayout] = useState<RhsLayoutState>(() => {
    if (typeof window === 'undefined') {
      return { sidebarWidth: DEFAULT_SIDEBAR_WIDTH, fleetFraction: DEFAULT_FLEET_FRACTION };
    }
    return loadRhsLayout(window.localStorage).state;
  });
  const [connected, setConnected] = useState(pixelFixtureActive());
  const layoutRef = useRef(layout);
  const dragRef = useRef<{ kind: 'width' | 'fleet' } | null>(null);
  const mainRef = useRef<HTMLDivElement>(null);
  const splitRef = useRef<HTMLDivElement>(null);
  layoutRef.current = layout;
  const layoutStyles = stylesForState(layout);

  useEffect(() => {
    const pref = readThemePref();
    setTheme(pref);
    applyTheme(pref);
    setLayout(loadRhsLayout(window.localStorage).state);
  }, []);

  useEffect(() => {
    const on = () => setConnected(true);
    const off = () => setConnected(false);
    mux.onOpen = on;
    mux.onClose = off;
    return () => {
      mux.onOpen = undefined;
      mux.onClose = undefined;
    };
  }, [mux]);

  useEffect(() => {
    const onMove = (e: PointerEvent) => {
      const d = dragRef.current;
      if (!d) return;
      const cur = layoutRef.current;
      if (d.kind === 'width' && mainRef.current) {
        const rect = mainRef.current.getBoundingClientRect();
        const w = sidebarWidthFromPointer(e.clientX - rect.left, rect.width);
        setLayout({ sidebarWidth: w, fleetFraction: cur.fleetFraction });
      } else if (d.kind === 'fleet' && splitRef.current) {
        const rect = splitRef.current.getBoundingClientRect();
        const f = fleetFractionFromPointer(e.clientY - rect.top, rect.height);
        setLayout({ sidebarWidth: cur.sidebarWidth, fleetFraction: f });
      }
    };
    const onUp = () => {
      if (!dragRef.current) return;
      dragRef.current = null;
      saveRhsLayout(window.localStorage, layoutRef.current);
      document.body.classList.remove('rhs-resizing', 'rhs-resizing-col', 'rhs-resizing-row');
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    return () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };
  }, []);

  return (
    <FrontierRowsContext.Provider value={frontierRows}>
      <div id="status">
        <span className={connected ? 'dot on' : 'dot off'} id="dot" />
        <span id="status-text">{connected ? 'connected' : 'connecting'}</span>
        <span id="voice-status">
          <span className="voice-dot" />
          <span id="voice-status-text">listening</span>
        </span>
        <PlanUsageBar />
        <div id="theme-toggle">
          {(['light', 'system', 'dark'] as ThemePref[]).map((p) => (
            <button
              key={p}
              type="button"
              data-t={p}
              title={p === 'light' ? 'Light' : p === 'dark' ? 'Dark' : 'System'}
              className={theme === p ? 'active' : ''}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                persistTheme(p);
                setTheme(p);
              }}
            >
              {p === 'light' ? '\u263C' : p === 'system' ? '\u25D0' : '\u263E'}
            </button>
          ))}
        </div>
      </div>
      <div id="degraded-banner" className={degraded ? 'visible' : undefined} role="status" aria-live="polite">
        {degraded}
      </div>
      <div id="idle-storm-banner" role="status" aria-live="polite" />
      <div id="main" ref={mainRef}>
        <AgentInteraction mux={mux} name="jevons" title="Root" density="comfortable" onMeta={onJevonsMeta} />
        <div id="activity-pane" style={{ width: layoutStyles.sidebarWidthPx, flexShrink: 0 }}>
          <div
            id="rhs-width-handle"
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar width"
            tabIndex={0}
            onPointerDown={(e) => {
              if (e.button !== 0) return;
              dragRef.current = { kind: 'width' };
              document.body.classList.add('rhs-resizing', 'rhs-resizing-col');
            }}
          />
          <div id="cost-ticker" title="Token burn rate — click for detail" />
          <div id="activity-header" className="agents-header">
            <span className="ah-label">Agents</span>
            <button type="button" id="aside-history-btn" title="Browse closed / dismissed asides">
              Closed
            </button>
            <button type="button" id="open-viz-btn" title="Open project graph viz panel">
              Open viz
            </button>
          </div>
          <div id="aside-history-panel" role="region" aria-label="Closed asides history" hidden>
            <div className="aside-history-head">
              <span className="ah-hist-label">Closed asides</span>
            </div>
          </div>
          <div id="rhs-split" ref={splitRef}>
            <div
              id="agents"
              style={{ flex: '0 0 ' + layoutStyles.fleetFlexBasis, minHeight: 60, maxHeight: 'none' }}
            >
              <AgentTree
                agents={agents}
                selected={fixture ? '' : agent}
                onSelect={(name) => navigate({ search: { agent: name, tab } })}
              />
            </div>
            <div
              id="rhs-split-handle"
              role="separator"
              aria-orientation="horizontal"
              aria-label="Resize fleet and frontier panels"
              tabIndex={0}
              onPointerDown={(e) => {
                if (e.button !== 0) return;
                dragRef.current = { kind: 'fleet' };
                document.body.classList.add('rhs-resizing', 'rhs-resizing-row');
              }}
            />
            <SidebarPanel
              tab={tab}
              readyCount={fixture ? PIXEL_FIXTURE_READY : frontierRows.length}
              onTab={(next) => navigate({ search: { agent, tab: next } })}
              onGraph={() => {
                setGraphOpen(true);
                setGraphNonce((n) => n + 1);
              }}
              transcript={
                <AgentInteraction mux={mux} name={agent} title={agent} density="compact" />
              }
            >
              <FrontierTable rows={frontierRows} />
            </SidebarPanel>
          </div>
          <div id="activity-header" style={{ marginTop: 0 }}>
            Workers <span id="workers-live">NONE YET</span>
          </div>
          <div id="workers" title="jwork workers" />
        </div>
      </div>
      <MermaidVizPanel open={graphOpen} graphNonce={graphNonce} onClose={() => setGraphOpen(false)} />
    </FrontierRowsContext.Provider>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
