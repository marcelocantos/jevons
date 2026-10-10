// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { keepPreviousData, QueryClient, QueryClientProvider, useQuery, useQueryClient } from '@tanstack/react-query';
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
import { FrontierTable } from './components/FrontierTable';
import { FrontierRowsContext } from './frontier/rows';
import { useSeatFrontier } from './frontier/useSeatFrontier';
import { PlanUsageBar } from './components/PlanUsageBar';
// T988: the Workers/jwork strip is unmounted, not deleted. Restore by
// re-enabling this import and the <WorkersList /> mount below.
// import { WorkersList } from './components/WorkersList';
import { AsideHistoryPanel } from './components/AsideHistoryPanel';
import { SettingsIcon, ThemeIcon } from './components/HeaderIcons';
import { MermaidVizPanel } from './components/MermaidVizPanel';
import {
  applyTheme,
  nextThemePref,
  persistTheme,
  readThemePref,
  systemAppearance,
  themeLabel,
  type ThemePref,
} from './theme';
import {
  DEFAULT_FLEET_FRACTION,
  defaultState,
  DEFAULT_SIDEBAR_WIDTH,
  fleetFractionFromPointer,
  load as loadRhsLayout,
  save as saveRhsLayout,
  sidebarWidthFromPointer,
  stylesForState,
  type RhsLayoutState,
} from './layout/rhsLayout';
import { mergeAgentChrome } from './plan/modelPrefix';
import { planTargetAskFocus } from './frontier/targetAsk';
import { TargetAskContext, type TargetAskHost } from './frontier/targetAskContext';
import { useCockpitKeys } from './keys/useCockpitKeys';
import { focusMainComposer } from './keys/composerFocus';
import { ReviewDetail, ReviewsList, useReviews } from './reviews/Reviews';
import { ImageLightbox } from './components/ImageLightbox';
import { CockpitSettings } from './components/CockpitSettings';


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
    typeof raw.agent === 'string' ? raw.agent.trim() : '';
  const tab: SidebarTab =
    raw.tab === 'transcript' || raw.tab === 'coach' || raw.tab === 'reviews' ? raw.tab : 'frontier';
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

const reviewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/reviews/$reviewId',
  component: ReviewPage,
});

function ReviewPage() {
  const { reviewId } = reviewRoute.useParams();
  const navigate = useNavigate();
  return <ReviewDetail id={reviewId} onBack={() => void navigate({ to: '/', search: { agent: '', tab: 'reviews' } })}
    onAnswer={() => {
      void navigate({ to: '/', search: { agent: '', tab: 'reviews' } }).then(() => {
        focusMainComposer();
      });
    }} />;
}

const routeTree = rootRoute.addChildren([indexRoute, reviewRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

function Cockpit() {
  const mux = getMux();
  const { agent, tab } = indexRoute.useSearch();
  useCockpitKeys();
  const navigate = useNavigate({ from: indexRoute.fullPath });
  const lastAgentsRef = useRef<AgentRow[]>([]);
  const [degraded, setDegraded] = useState('');
  const [graphOpen, setGraphOpen] = useState(false);
  const [graphNonce, setGraphNonce] = useState(0);
  const [asideOpen, setAsideOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  // Width, not user agent: a folded phone and a narrow browser need the same
  // navigation, whereas an unfolded foldable/tablet keeps the split view.
  const [narrow, setNarrow] = useState(() => window.matchMedia('(max-width: 599px)').matches);
  const [agentsOpen, setAgentsOpen] = useState(false);
  const agentsButton = useRef<HTMLButtonElement>(null);
  const activityRef = useRef<HTMLDivElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const closeAgents = useCallback(() => {
    setAgentsOpen(false);
    agentsButton.current?.focus();
  }, []);
  useEffect(() => {
    const media = window.matchMedia('(max-width: 599px)');
    const onChange = () => {
      setNarrow(media.matches);
      setAgentsOpen(false);
      if (activityRef.current?.contains(document.activeElement)) {
        if (media.matches) agentsButton.current?.focus();
        else focusMainComposer();
      }
    };
    media.addEventListener('change', onChange);
    return () => media.removeEventListener('change', onChange);
  }, []);
  useEffect(() => {
    if (!narrow || !agentsOpen) return;
    closeButton.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); closeAgents(); }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [narrow, agentsOpen, closeAgents]);
  const settingsButton = useRef<HTMLButtonElement>(null);
  const openGraph = useCallback(() => {
    setGraphOpen(true);
    setGraphNonce((n) => n + 1);
  }, []);
  const onJevonsMeta = useCallback((meta: ConversationMeta | null) => {
    setDegraded(degradedBannerText(meta));
  }, []);
  const queryClient = useQueryClient();
  // 🎯T269: × on an aside row → DELETE /api/asides/<name>. 404 = already gone
  // (idempotent); refreshing the agents query is the owner-visible truth (T164).
  const dismissFleetAside = useCallback(
    async (name: string) => {
      if (!name) return;
      const r = await fetch('/api/asides/' + encodeURIComponent(name), { method: 'DELETE' });
      if (!r.ok && r.status !== 404) {
        throw new Error((await r.text()) || 'aside dismiss HTTP ' + r.status);
      }
      lastAgentsRef.current = lastAgentsRef.current.filter((a) => a.name !== name);
      await queryClient.invalidateQueries({ queryKey: ['agents'] });
      focusMainComposer();
    },
    [queryClient],
  );
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
          target_id?: string;
          ledger?: string;
          stop_reason?: string;
          starting?: boolean;
          stopped_at?: string;
          mass_stop?: string;
          rehydrate?: string;
          reauth_available?: boolean;
          plan_wall?: string;
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
          target_id: a.target_id,
          ledger: a.ledger,
          stop_reason: a.stop_reason,
          starting: a.starting,
          stopped_at: a.stopped_at,
          mass_stop: a.mass_stop,
          rehydrate: a.rehydrate,
          reauth_available: a.reauth_available,
          plan_wall: a.plan_wall,
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
  const agents = agentsQ.data && agentsQ.data.length
    ? agentsQ.data
    : [{ name: 'jevons' }, { name: 'jevons-po' }];
  const reviewsQ = useReviews();
  const openReview = (id: string) => void navigate({ to: '/reviews/$reviewId', params: { reviewId: id } });
  const frontierCwd = agents.find((a) => a.name === agent)?.workdir?.trim() || '';
  const frontierQ = useSeatFrontier(frontierCwd);
  const frontierNote = !frontierCwd || frontierQ.data
    ? undefined
    : frontierQ.isError
      ? 'frontier unavailable'
      : 'loading…';
  const frontierRows = frontierCwd ? frontierQ.data?.rows || [] : [];
  const frontierLedger = frontierCwd ? frontierQ.data?.ledgerKey || '' : '';
  // 🎯T267: live target-ask → select owning PO (T253 rebinds Frontier) + highlight row.
  const [frontierHighlightId, setFrontierHighlightId] = useState('');
  const askHost = useMemo<TargetAskHost>(
    () => ({
      agents,
      selectedAgent: agent,
      onTargetAsk: (text: string) => {
        const plan = planTargetAskFocus({ text, agents, selectedAgent: agent });
        if (!plan) return;
        setFrontierHighlightId(plan.highlightId);
        navigate({ search: { agent: plan.po, tab: plan.tab } });
      },
    }),
    [agents, agent, navigate],
  );
  const [theme, setTheme] = useState<ThemePref>('system');
  const [layout, setLayout] = useState<RhsLayoutState>(() => {
    if (typeof window === 'undefined') {
      return { sidebarWidth: DEFAULT_SIDEBAR_WIDTH, fleetFraction: DEFAULT_FLEET_FRACTION };
    }
    return loadRhsLayout(window.localStorage).state;
  });
  const [connected, setConnected] = useState(false);
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
    <TargetAskContext.Provider value={askHost}>
      <div id="status">
        <button ref={agentsButton} id="agents-toggle" type="button" aria-label="Agents"
          aria-controls="activity-pane" aria-expanded={narrow && agentsOpen}
          onClick={() => agentsOpen ? closeAgents() : setAgentsOpen(true)}>Agents</button>
        <span className={connected ? 'dot on' : 'dot off'} id="dot" />
        <span id="voice-status">
          <span className="voice-dot" />
          <span id="voice-status-text">listening</span>
        </span>
        <PlanUsageBar
          mux={mux}
          refusedSeats={agents.filter((a) => a.reauth_available).map((a) => ({ name: a.name, provider: a.provider || '' }))}
        />
        <button ref={settingsButton} id="settings-button" type="button" title="Settings"
          aria-label="Open settings" aria-haspopup="dialog" aria-expanded={settingsOpen}
          onClick={() => setSettingsOpen(true)}>
          <SettingsIcon />
        </button>
        <button
          id="theme-toggle"
          type="button"
          data-t={theme}
          title={`${themeLabel(theme)} — click for ${themeLabel(nextThemePref(theme, systemAppearance()))}`}
          aria-label={`Theme: ${themeLabel(theme)}`}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            const next = nextThemePref(theme, systemAppearance());
            persistTheme(next);
            setTheme(next);
          }}
        >
          <ThemeIcon theme={theme} />
        </button>
      </div>
      <div id="degraded-banner" className={degraded ? 'visible' : undefined} role="status" aria-live="polite">
        {degraded}
      </div>
      <div id="idle-storm-banner" role="status" aria-live="polite" />
      <div id="main" ref={mainRef}>
        <AgentInteraction mux={mux} name="jevons" title="Root" density="comfortable" connected={connected} onMeta={onJevonsMeta} />
        {narrow && agentsOpen && <button id="agents-backdrop" type="button" aria-label="Close Agents" onClick={closeAgents} />}
        <div id="activity-pane" ref={activityRef} data-drawer-open={narrow && agentsOpen}
          inert={narrow && !agentsOpen} style={{ width: layoutStyles.sidebarWidthPx, flexShrink: 0 }}>
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
          <button ref={closeButton} type="button" id="agents-close" aria-label="Close Agents" onClick={closeAgents}>Close</button>
          <div id="cost-ticker" title="Token burn rate — click for detail" />
          <div id="activity-header" className="agents-header">
            <span className="ah-label">Agents</span>
            <button
              type="button"
              id="aside-history-btn"
              title="Browse closed / dismissed asides"
              aria-expanded={asideOpen}
              onClick={() => setAsideOpen((v) => !v)}
            >
              Closed
            </button>
            <button type="button" id="open-viz-btn" title="Open project graph viz panel" onClick={openGraph}>
              Open viz
            </button>
          </div>
          <AsideHistoryPanel open={asideOpen} onClose={() => setAsideOpen(false)} />
          <div id="rhs-split" ref={splitRef}>
            <div
              id="agents"
              style={{ flex: '0 0 ' + layoutStyles.fleetFlexBasis, minHeight: 60, maxHeight: 'none' }}
            >
              <AgentTree
                agents={agents}
                selected={agent}
                onSelect={(name) => navigate({ search: { agent: name === agent ? '' : name, tab } })}
                onDismiss={(name) => void dismissFleetAside(name)}
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
              reviewCount={reviewsQ.data?.length}
              readyCount={!frontierCwd || frontierQ.data ? frontierRows.length : undefined}
              readyNote={frontierNote}
              onTab={(next) => {
                navigate({ search: { agent, tab: next } });
                queueMicrotask(() => focusMainComposer());
              }}
              onGraph={openGraph}
              onRefresh={() => void queryClient.invalidateQueries({ queryKey: ['frontier'] })}
              reviews={<ReviewsList onOpen={openReview} />}
              transcript={
                agent === 'jevons' ? (
                  <div
                    id="agent-inspect"
                    className={
                      'rhs-tab-pane conversation-widget density-compact' +
                      (tab === 'transcript' ? ' active' : '')
                    }
                    data-density="compact"
                    data-agent-id="jevons"
                  >
                    <div id="agent-inspect-header">
                      <span className="ai-label">Transcript</span>
                      <span className="ai-name" id="agent-inspect-name">
                        jevons
                      </span>
                    </div>
                    <p className="ai-empty">Root transcript is the main pane.</p>
                  </div>
                ) : (
                  <AgentInteraction
                    mux={mux}
                    name={agent}
                    title={agent}
                    density="compact"
                    paneActive={tab === 'transcript'}
                    planWall={agents.find((a) => a.name === agent)?.plan_wall}
                  />
                )
              }
            >
              {frontierNote ? <div className="frontier-note" role="status">{frontierNote}</div> : null}
              <FrontierTable key={frontierCwd} ledgerKey={frontierLedger} rows={frontierRows} agents={agents} selectedAgent={agent} frontierLedger={frontierLedger} highlightId={frontierHighlightId} />
            </SidebarPanel>
          </div>
          {/* T988: <WorkersList /> unmounted — see the import note above. */}
        </div>
      </div>
      <MermaidVizPanel open={graphOpen} graphNonce={graphNonce} onClose={() => setGraphOpen(false)} />
      <CockpitSettings open={settingsOpen} connected={connected} theme={theme}
        onClose={() => { setSettingsOpen(false); settingsButton.current?.focus(); }}
        onTheme={(pref) => { persistTheme(pref); setTheme(pref); }}
        onResetLayout={() => {
          const next = defaultState();
          setLayout(next);
          saveRhsLayout(window.localStorage, next);
        }} />
      <ImageLightbox />
    </TargetAskContext.Provider>
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
