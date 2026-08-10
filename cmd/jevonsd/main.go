package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/audit"
	"github.com/marcelocantos/jevons/internal/auth"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/converge/sticky"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/doit"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/research"
	"github.com/marcelocantos/jevons/internal/rsi"
	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
	"github.com/marcelocantos/jevons/internal/upgrade"
	"github.com/marcelocantos/jevons/internal/workers"
	"github.com/marcelocantos/jevons/internal/writconf"

	"github.com/marcelocantos/pigeon"
	"github.com/marcelocantos/pigeon/crypto"
	"github.com/marcelocantos/pigeon/qr"
)

func main() {
	configPath := flag.String("config", "", "config file (default ~/.jevons/config.yaml; missing file = built-in defaults)")
	port := flag.Int("port", 13705, "listen port")
	bindAddr := flag.String("bind", "", "listen interface (default 127.0.0.1 — loopback only; remote devices use the pigeon relay)")
	relayURL := flag.String("relay", "", "relay URL to register with (e.g. https://carrier-pigeon.fly.dev)")
	relayToken := flag.String("relay-token", "", "bearer token for relay authentication (or set TERN_TOKEN env var)")
	relayInstanceID := flag.String("instance-id", "", "persistent relay instance ID (enables reconnect without re-pairing)")
	workDir := flag.String("workdir", ".", "default working directory for worker sessions")
	model := flag.String("model", "", "default model for workers (empty = provider default)")
	providerFlag := flag.String("provider", "", "default agent backend (grok, claude, …; empty = config.yaml / JEVONS_PROVIDER / grok)")
	overseerModel := flag.String("jevons-model", "", "model for the overseer agent (empty = same as -model)")
	debug := flag.Bool("debug", false, "enable debug logging")
	enableTLS := flag.Bool("tls", false, "enable mTLS on the HTTP listener (requires client certs after provisioning)")
	setOpenAIKey := flag.Bool("set-openai-key", false, "prompt for OpenAI API key, store in macOS Keychain, and exit")
	setXAIKey := flag.Bool("set-xai-key", false, "prompt for xAI API key, store in macOS Keychain, and exit")
	pairInstance := flag.String("pair", "", "mint a PairingArtifact for the given peer instance ID (requires --relay), print QR + artifact on stdout, and exit")
	addCredential := flag.String("add-credential", "", "load a server-side PairingRecord from the given JSON file into the credential store, then exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	helpAgent := flag.Bool("help-agent", false, "print agent guide and exit")
	flag.Parse()

	if *setOpenAIKey {
		promptAndStoreKey("OpenAI API", "openai-api-key")
	}
	if *setXAIKey {
		promptAndStoreKey("xAI API", "xai-api-key")
	}

	if *showVersion {
		fmt.Println("jevonsd", cli.Version)
		os.Exit(0)
	}
	if *helpAgent {
		flag.PrintDefaults()
		fmt.Println()
		fmt.Print(cli.AgentGuide)
		os.Exit(0)
	}

	if *pairInstance != "" || *addCredential != "" {
		runOneShot(*pairInstance, *addCredential, *relayURL)
		os.Exit(0)
	}

	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	// 🎯T282: drop any enclosing agent session's identity before anything
	// spawns. Started from inside a Claude Code session (make run,
	// make test-journey, an agent-driven dev loop), jevonsd would otherwise
	// hand CLAUDE_CODE_SESSION_ID and friends to every Claude agent it
	// launches, and those agents rejoin the parent session's bridge instead
	// of running as their own — a directed turn then never submits.
	if cleared := cli.ScrubInheritedSessionEnv(); len(cleared) > 0 {
		slog.Info("cleared enclosing agent session env so spawned agents start clean",
			"vars", cleared)
	}
	// 🎯T282: and reconcile the claudia tmux server, which hands each new
	// agent window the environment of whatever started the server —
	// possibly a test run from days ago, since the anchor session keeps it
	// alive across daemon restarts.
	if changed, err := cli.NormalizeAgentTmuxEnv(); err != nil {
		slog.Warn("could not reconcile claudia tmux server env; agents may inherit stale vars",
			"err", err)
	} else if len(changed) > 0 {
		slog.Info("reconciled claudia tmux server env with the daemon's",
			"vars", changed, "socket", cli.AgentTmuxSocket())
	}

	// Structured config (🎯T44): built-in defaults ← config.yaml ← flags.
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("cannot load config", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if explicit["port"] {
		cfg.Port = *port
	}
	if explicit["bind"] {
		cfg.BindAddr = *bindAddr
	}
	if explicit["workdir"] {
		cfg.WorkDir = *workDir
	}
	if explicit["model"] {
		cfg.Model = *model
	}
	if explicit["provider"] {
		cfg.Provider = *providerFlag
	}
	if explicit["jevons-model"] {
		cfg.OverseerModel = *overseerModel
	}
	// 🎯T148: resolve once at boot (config → JEVONS_PROVIDER → grok).
	defaultProvider := cli.ResolveProvider("", cfg.Provider)

	// Set up the overseer workdir with rendered persona instructions.
	jevDir := cfg.OverseerDir()
	if err := os.MkdirAll(jevDir, 0o755); err != nil {
		slog.Error("cannot create overseer workdir", "err", err)
		os.Exit(1)
	}
	persona, err := cfg.Persona()
	if err != nil {
		slog.Error("cannot render overseer persona", "err", err)
		os.Exit(1)
	}
	// Project instructions for the overseer (Grok reads AGENTS.md / Claude.md).
	for _, name := range []string{"AGENTS.md", "Claude.md"} {
		path := filepath.Join(jevDir, name)
		if err := os.WriteFile(path, []byte(persona), 0o644); err != nil {
			slog.Error("cannot write overseer instructions", "path", path, "err", err)
			os.Exit(1)
		}
	}

	// Overseer MCP is registered user-scoped for the selected provider
	// before launch (ensureOverseerMCPServer: Grok → ~/.grok/config.toml,
	// Claude → claude mcp -s user; 🎯T58 + 🎯T212). That attaches on both
	// session/new and session/load — so we no longer pass a session-scoped
	// ACP param (mcp.claudia.json), which Grok honours only on session/new.
	// Remove any stale copy so claudia doesn't double-register the same
	// server name. Use the concrete bind address, never "localhost": the
	// loopback-only default (🎯T6) binds IPv4 127.0.0.1, and localhost may
	// resolve to ::1.
	mcpHost := cfg.BindAddr
	if mcpHost == "" || mcpHost == "0.0.0.0" || mcpHost == "::" {
		mcpHost = "127.0.0.1"
	}
	_ = os.Remove(filepath.Join(jevDir, "mcp.claudia.json"))

	// Initialize CA for mTLS device provisioning.
	ca, err := auth.NewCA(cfg.StateDir)
	if err != nil {
		slog.Error("cannot initialize CA", "err", err)
		os.Exit(1)
	}

	// Multi-provider session roots (🎯T213): Grok sessions + Claude projects.
	sessionRoots := discovery.Roots{
		GrokSessions:   cfg.SessionsDir,
		ClaudeProjects: cfg.ClaudeProjects,
	}
	scanner := discovery.NewScannerRoots(sessionRoots)

	srv := server.New(cli.Version, cfg.StateDir)
	srv.SetOverseerName(cfg.OverseerName)
	// 🎯T200: declarative domain portfolios from config (no GM agent).
	srv.SetPortfolios(cfg.Portfolios)
	// 🎯T131: primary project workdir for bullseye frontier discovery (CLI open).
	if absWD, err := filepath.Abs(cfg.WorkDir); err == nil {
		srv.SetFrontierCwd(absWD)
	} else {
		srv.SetFrontierCwd(cfg.WorkDir)
	}

	// Durable conversation log (🎯T30.1): every chat line is fsynced here
	// and replayed to reconnecting clients — no conversation is ever lost.
	clog, err := chatlog.Open(filepath.Join(cfg.StateDir, "chatlog", cfg.OverseerName+".jsonl"))
	if err != nil {
		slog.Error("cannot open chat log", "err", err)
		os.Exit(1)
	}
	srv.SetChatLog(clog)
	// 🎯T124 / 🎯T213: RHS fleet transcript inspect reads Grok + Claude stores.
	srv.SetTranscriptReader(transcript.NewReaderRoots(sessionRoots))
	// 🎯T293 / 🎯T311: the RHS badge names the model an agent is RUNNING. Live
	// frames are the freshest source, but the hub they feed dies with the
	// daemon — so each provider's own session log (Grok turn_completed usage,
	// Claude message.model) re-seeds it at attach.
	srv.SetModelSessionRoots(sessionRoots)

	// Durable decision/lifecycle journal (🎯T120): browser + server events
	// under state_dir/logs/events.jsonl — tool-readable without privilege.
	var elog *eventlog.Journal
	if j, err := eventlog.Open(eventlog.DefaultPath(cfg.StateDir)); err != nil {
		slog.Error("cannot open event log", "err", err)
		// Non-fatal: continue with slog-only browser path.
	} else {
		elog = j
		srv.SetEventLog(elog)
		slog.Info("event log ready", "path", elog.Path())
	}

	srv.SetCA(ca)

	// Load OpenAI API key from Keychain (fall back to env var).
	if key, err := loadKeychainKey("openai-api-key"); err == nil && key != "" {
		srv.SetOpenAIKey(key)
		slog.Info("OpenAI API key loaded from Keychain")
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		srv.SetOpenAIKey(key)
		slog.Info("OpenAI API key loaded from environment")
	}

	// Load xAI API key for Grok Realtime voice bridge.
	var xaiKey string
	if key, err := loadKeychainKey("xai-api-key"); err == nil && key != "" {
		xaiKey = key
		slog.Info("xAI API key loaded from Keychain")
	} else if key := os.Getenv("XAI_API_KEY"); key != "" {
		xaiKey = key
		slog.Info("xAI API key loaded from environment")
	}
	if xaiKey != "" {
		vb := server.NewVoiceBridge(srv, xaiKey)
		srv.SetVoiceBridge(vb)
		slog.Info("Grok voice bridge configured")
	} else {
		slog.Info("no xAI API key — voice bridge disabled (set XAI_API_KEY or store via: security add-generic-password -a jevons -s xai-api-key -w YOUR_KEY)")
	}

	// 🎯T8.2 worker observability (SQLite workers/events + SSE hub).
	workerTracker, err := workers.NewTracker(filepath.Join(cfg.StateDir, "workers.db"))
	if err != nil {
		slog.Error("workers tracker unavailable — jwork observability disabled", "err", err)
		workerTracker = nil
	} else {
		defer workerTracker.Close()
		srv.SetWorkersTracker(workerTracker)
	}

	// 🎯T27.2: structured provider config + persistence home for the registry.
	// Desired set comes from config.yaml `providers:`; runtime state lives in
	// StateDir/providers/registry.db. Reconcile/supervisor is 🎯T27.3.
	// providerCfg is retained for the process lifetime so Reload remains
	// available when T27.3 wires reconcile / an admin surface.
	var providerCfg *provider.ConfigManager
	if pstore, err := provider.OpenStore(provider.DefaultStorePath(cfg.StateDir)); err != nil {
		slog.Error("provider store unavailable — external providers disabled", "err", err)
	} else {
		defer pstore.Close()
		pm, err := provider.NewConfigManager(provider.ConfigManagerArgs{
			ConfigPath: cfgPath,
			Store:      pstore,
		})
		if err != nil {
			slog.Error("provider config load failed — external providers disabled", "err", err)
		} else {
			providerCfg = pm
			slog.Info("provider config ready",
				"path", providerCfg.ConfigPath(),
				"store", pstore.Path(),
				"desired", len(providerCfg.Desired()),
				"enabled", len(providerCfg.Enabled()),
			)
		}
	}
	// providerCfg feeds the 🎯T27.3/T27.4 lifecycle + aggregator wiring
	// below, once the MCP server (the aggregation sink) exists.

	// 🎯T8.3 execution safety (doit Engine: L1/L2/L3, audit, capabilities).
	// L3 stays off by default so boot is hermetic without an LLM; L1/L2 +
	// capability registry + hash-chained audit are always on under StateDir.
	var doitEng *doit.Engine
	if eng, err := doit.Open(doit.OpenArgs{
		StateDir:      filepath.Join(cfg.StateDir, "doit"),
		Level3Enabled: false,
	}); err != nil {
		slog.Error("doit engine unavailable — jwork policy gate disabled", "err", err)
	} else {
		doitEng = eng
		defer eng.Close()
		slog.Info("doit engine ready",
			"audit", eng.AuditPath(),
			"capabilities", len(eng.ListCapabilities()),
		)
	}

	// Worker completion events are delivered to the overseer via the
	// registry agent's Send (wired below in SetNotify).
	var registry *claudia.Registry
	mcpSrv := mcpserver.New(cfg.WorkDir, func() (string, error) {
		return srv.RequestScreenshot(10 * time.Second)
	}, &mcpserver.TranscriptOps{
		Read: func(sessionID string) ([]map[string]any, error) {
			tr := transcript.NewReaderRoots(sessionRoots)
			return tr.Read(sessionID)
		},
		Truncate: func(sessionID string, keepTurns int) error {
			tr := transcript.NewReaderRoots(sessionRoots)
			return tr.Truncate(sessionID, keepTurns)
		},
		GetID: func() string {
			if proc := registry.Get(cfg.OverseerName); proc != nil {
				return proc.SessionID()
			}
			return ""
		},
	})
	if workerTracker != nil {
		mcpSrv.SetWorkersTracker(workerTracker)
	}
	if doitEng != nil {
		mcpSrv.SetDoitEngine(doitEng)
	}

	// 🎯T27.3/🎯T27.4/🎯T27.5: provider supervisor + MCP tool aggregation +
	// feed ingestion. Lifecycle converges processes/attachments; aggregator
	// mirrors Running providers' MCP endpoints into /mcp; FeedHub serves
	// /ws/provider, folds events into the live model, and Broadcasts to
	// clients. ConfigManager.Reload re-converges all three.
	var liveMon *provider.LivenessMonitor
	if providerCfg != nil {
		provAgg := provider.NewAggregator(provider.AggregatorArgs{Sink: mcpSrv})
		provLC := provider.NewLifecycle(provider.LifecycleArgs{OnPhase: provAgg.HandlePhase})
		feedReg := provider.NewRegistry()
		feedReg.HubID = "jevons" // loop-safety (§5.4): drop self-origin relays
		feedHub := provider.NewFeedHub(provider.FeedHubArgs{
			Registry: feedReg,
			Store:    providerCfg.Store(),
			OnEvent: func(id string, ev provider.FeedEvent) {
				srv.Broadcast(map[string]any{
					"type":     "provider_event",
					"provider": id,
					"event":    ev,
				})
			},
			OnStatus: func(st provider.FeedStatus) {
				srv.Broadcast(map[string]any{
					"type":   "provider_status",
					"status": st,
				})
			},
			// 🎯T27.6: recompose and push the provider view on UI-relevant
			// changes (surface attach, bound-feed events).
			OnUI: srv.BroadcastProviderView,
			Allowed: func(id string) bool {
				for _, d := range providerCfg.Enabled() {
					if d.ID == id {
						return true
					}
				}
				return false
			},
		})
		provAgg.SetDecls(providerCfg.Desired())
		feedHub.SetDecls(providerCfg.Desired())
		srv.SetProviderFeeds(feedHub)
		srv.SetProviderHealth(provLC.Health)

		// 🎯T27.9: automation liveness — declared automations are checked
		// against their signal sources on an interval; a stall folds into
		// the aggregated model (liveness/automations), broadcasts as a
		// provider_event, and notifies the owner via the overseer.
		if len(cfg.Automations) > 0 {
			liveMon = provider.NewLivenessMonitor(provider.LivenessMonitorArgs{
				Decls:    cfg.Automations,
				Registry: feedReg,
				OnEvent: func(ev provider.FeedEvent) {
					srv.Broadcast(map[string]any{
						"type":     "provider_event",
						"provider": provider.LivenessProviderID,
						"event":    ev,
					})
				},
				OnNotice: func(st provider.AutomationStatus) {
					if err := srv.SendToOverseer(provider.FormatAutomationNotice(st)); err != nil {
						slog.Warn("automation notice delivery failed", "automation", st.ID, "err", err)
					}
				},
			})
			srv.SetAutomations(liveMon.Statuses)
			slog.Info("automation liveness ready", "tracked", len(cfg.Automations))
		}
		providerCfg.SetOnChange(func(decls []config.ProviderDecl) {
			provAgg.SetDecls(decls)
			feedHub.SetDecls(decls)
			if err := provLC.Reconcile(decls); err != nil {
				slog.Error("provider reconcile after config reload failed", "err", err)
			}
		})
		if err := provLC.Reconcile(providerCfg.Desired()); err != nil {
			slog.Error("provider reconcile failed", "err", err)
		}
		slog.Info("provider feed hub ready", "desired", len(providerCfg.Desired()))
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := provLC.Shutdown(ctx); err != nil {
				slog.Warn("provider lifecycle shutdown", "err", err)
			}
			provAgg.Shutdown()
		}()
	}

	// 🎯T335: writ confinement substrate (CLI) — seatbelt + egress for high-risk exec.
	writBin := writconf.ResolveWritBin()
	writAvail := writBin != ""
	var writEx writconf.Executor
	if writAvail {
		writEx = &writconf.CLIExecutor{Bin: writBin}
		slog.Info("writ confinement ready", "bin", writBin)
	} else {
		writEx = writconf.PureExecutor{}
		slog.Warn("writ binary not found — confined-exec uses pure allowlist only until writ is on PATH")
	}
	mcpSrv.SetWritExecutor(writEx, writBin, writAvail)
	srv.SetWritExecutor(writEx, writBin, writAvail)

	mux := http.NewServeMux()

	// Dev server: serve web/ from disk with hot reload.
	// Registered first — GET / is a catch-all fallback;
	// more specific routes registered after take precedence.
	webDir := filepath.Join(filepath.Dir(os.Args[0]), "..", "web")
	if abs, err := filepath.Abs(webDir); err == nil {
		webDir = abs
	}
	devSrv := server.RegisterUIRoutes(mux, webDir)

	srv.RegisterRoutes(mux)
	mcpSrv.RegisterRoutes(mux)
	if devSrv != nil {
		go func() {
			if err := devSrv.Watch(); err != nil {
				slog.Error("dev server watch failed", "err", err)
			}
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 🎯T27.9: start the liveness check loop once the daemon context exists.
	if liveMon != nil {
		go liveMon.Run(ctx)
	}

	// 🎯T40: SIGINT/SIGTERM = normal stop (StopAll); SIGHUP = upgrade exit
	// (skip StopAll, write handles). JEVONS_UPGRADE_EXIT=1 also marks
	// SIGTERM/SIGINT as upgrade when launchd cannot send SIGHUP.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// 🎯T40: default Grok connect-mode so agents use detached `grok agent
	// serve` + WebSocket and survive coordinator-only upgrades. Override
	// with CLAUDIA_GROK_CONNECT=0 to force parent-owned stdio.
	if v := os.Getenv("CLAUDIA_GROK_CONNECT"); v == "" {
		_ = os.Setenv("CLAUDIA_GROK_CONNECT", "1")
	}

	// Agent registry — manages persistent Grok ACP sessions.
	registryPath := filepath.Join(cfg.StateDir, "agents.json")
	{
		r, err := claudia.NewRegistry(registryPath)
		if err != nil {
			slog.Error("agent registry failed", "err", err)
			os.Exit(1)
		}
		registry = r
	}

	// Prior upgrade handoff: session_ids + optional connect-mode endpoints.
	// Process reattach when handles carry ConnectURL+PID (agents.json also
	// persists those fields from the last Launch).
	if snap, err := upgrade.LoadSnapshot(upgrade.SnapshotPath(cfg.StateDir)); err != nil {
		slog.Error("upgrade handles load failed (malformed handoff is hard error)", "err", err)
		os.Exit(1)
	} else if snap != nil {
		plan := upgrade.PlanReattach(snap)
		// Merge connect endpoints into registry defs before StartAll so
		// Launch reattaches to still-running serve processes.
		for _, h := range snap.Agents {
			if h.ConnectURL == "" || h.Name == "" {
				continue
			}
			if def := registry.Def(h.Name); def != nil {
				def.ConnectURL = h.ConnectURL
				def.ConnectPID = h.PID
				def.GrokConnect = true
				if h.SessionID != "" {
					def.SessionID = h.SessionID
				}
				if err := registry.Register(*def); err != nil {
					slog.Warn("upgrade handoff: could not merge connect endpoint", "agent", h.Name, "err", err)
				}
			}
		}
		slog.Info("upgrade handoff present",
			"sessions", len(plan.SessionIDs),
			"process_reattach", plan.ProcessReattachPossible,
			"written_at", snap.WrittenAt,
			"residual", plan.Residual)
	}

	// Wire registry and scanner into MCP server.
	mcpSrv.SetRegistry(registry)
	mcpSrv.SetScanner(scanner)
	// 🎯T110 self-test packs — same in-process env as POST /api/self_test/run.
	mcpSrv.SetSelfTestEnv(srv.SelfTestEnv)
	// 🎯T120: product log introspection (durable events.jsonl).
	mcpSrv.SetEventLogTailer(func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		ev, err := srv.TailEventLog(opt)
		return ev, srv.EventLogPath(), err
	})
	// 🎯T128.4: fleet MCP tools dual-write lifecycle events (source=server)
	// into the same journal so GET /api/logs / jevons_logs_tail see them.
	mcpSrv.SetEventLogger(srv.LogEvent)
	if elog != nil {
		mcpSrv.SetEventJournal(elog)
	}

	// Butler: durable-thread orchestrator over the thread store, the
	// session scanner (non-invasive observation), and the transcript
	// reader (status derivation). Threads persist across restarts so the
	// full set is never lost.
	threadStore, err := thread.NewStore(filepath.Join(cfg.StateDir, "threads.json"))
	if err != nil {
		slog.Error("thread store failed", "err", err)
		os.Exit(1)
	}
	// Token-spend clamp-down (🎯T36): start it before the butler so its
	// spawn/resume guards feed the butler's lifecycle. Returns nil if the
	// usage DB is unavailable — the cockpit still runs, just unguarded.
	guard := startCostGuard(ctx, cfg, registry, scanner, srv)

	// 🎯T114: same Claudia adapter is Fleet (threads) and Participants
	// (agent-only names) so Deliver/PushEvent share one id space.
	// 🎯T148: pluggable default provider for new threads/agents.
	fleetAdapter := fleet.NewClaudia(registry)
	fleetAdapter.SetDefaultProvider(defaultProvider)
	// 🎯T285: provider migration needs the session roots to find a
	// predecessor's transcript, and a durable store to hold that pointer
	// across the rotation that overwrites the old session id.
	fleetAdapter.SetSessionRoots(sessionRoots)
	fleetAdapter.SetHandoverStore(handover.NewStore(filepath.Join(cfg.StateDir, "handover")))
	mcpSrv.SetDefaultProvider(string(defaultProvider))
	// 🎯T325.3: durable idea intake ledger (state_dir/ideas.json).
	mcpSrv.SetIdeaStateDir(cfg.StateDir)
	// 🎯T388: durable agent-report store (state_dir/agent-reports/) so a
	// terminal report survives the 🎯T165/T195 reap of the agent that wrote it,
	// and an over-bound delivery can name a call that returns the whole text.
	mcpSrv.SetAgentReportDir(cfg.StateDir)
	// 🎯T392.2: coalesce machine-generated wakes into one digest per
	// recipient. Owner turns and worker replies are never batched — only
	// events the fleet generates about itself, whose content is additive.
	mcpSrv.SetWakeBatchWindow(mcpserver.DefaultWakeBatchWindow)
	// 🎯T325.2: task-type → harness routing seed. The compiled default is a
	// design-map seed, not owner policy: when a subscription runs dry the
	// owner must be able to steer new mints without a rebuild, so an
	// optional state_dir/llm-portfolio.json overrides it (missing file =
	// compiled seed).
	portfolioPath := filepath.Join(cfg.StateDir, "llm-portfolio.json")
	if pf, err := cost.LoadPortfolioFile(portfolioPath); err != nil {
		slog.Error("llm portfolio override unreadable — using compiled seed",
			"path", portfolioPath, "err", err)
	} else {
		mcpSrv.SetLLMPortfolio(pf)
		slog.Info("llm portfolio routing seed", "path", portfolioPath,
			"default_provider", pf.DefaultProvider)
	}
	// 🎯T325.2: multi-provider portfolio soft-cap overlays from budget.json
	// (session counts only; independent of cost disabled / subscription USD).
	if bcfg, err := cost.LoadBudgetConfig(filepath.Join(cfg.StateDir, "budget.json")); err == nil && len(bcfg.ProviderSoftCaps) > 0 {
		mcpSrv.SetProviderSoftCaps(bcfg.ProviderSoftCaps)
		slog.Info("llm portfolio soft caps from budget", "caps", bcfg.ProviderSoftCaps)
	}
	// 🎯T285: jevons_agent_migrate moves an agent between backends and
	// hands the successor its predecessor's transcript.
	mcpSrv.SetMigrator(fleetAdapter)
	srv.SetOverseerMigrator(fleetAdapter)
	srv.SetDefaultProvider(defaultProvider)
	btlrCfg := butler.Config{
		Store:        threadStore,
		Scanner:      scanner,
		Reader:       transcript.NewReaderRoots(sessionRoots),
		Fleet:        fleetAdapter,
		Participants: fleetAdapter,
	}
	if guard != nil {
		btlrCfg.SpawnGuard = guard.enforcer.AllowSpawn
		btlrCfg.ResumeGuard = guard.enforcer.AllowResume
	}
	btlr := butler.New(btlrCfg)
	mcpSrv.SetButler(btlr)

	// Wire the live cost view into the MCP tool, the web /api/cost
	// endpoint, and the owner-activity heartbeat that arms the dead-man.
	// Budget guards also cover the primary MCP spawn tools (jwork /
	// create_session / agent_start) so spawnHalted cannot be bypassed
	// outside the butler (T36.1 / Fable F2).
	if guard != nil {
		mcpSrv.SetBudgetGuards(guard.enforcer.AllowSpawn, guard.enforcer.AllowResume)
		mcpSrv.SetCostMonitor(guard.monitor.Snapshot)
		srv.SetCostSource(mcpSrv.CostJSON)
		srv.SetActivityHook(guard.enforcer.Heartbeat)
	}

	// 🎯T243 ambient RSI coach (product path): judgments → overseer.
	// Residual 🎯T92 mint remains opt-in (JEVONS_RSI_MINT / DEEPER).
	// 🎯T359: holistic background admission — ambient cycles ask before each
	// tick so owner turns and open Build missions keep their room.
	capGov := startCapacityGovernor(cfg, guard, mcpSrv, srv)

	// 🎯T392.1: bound the conversation every agent carries into a model
	// call. Deliberately not gated by the capacity governor — this loop
	// exists to *reduce* spend, so deferring it when budget is tight is
	// exactly backwards.
	startContextCeiling(ctx, cfg, sessionRoots, registry, fleetAdapter, srv)

	rsiCoach := startAmbientRSICoach(ctx, cfg, mcpSrv, capacityGate(capGov))
	rsiLoop := startAmbientRSIMint(ctx, cfg, mcpSrv)

	// 🎯T356 ambient research: periodic context refresh + async feed triggers,
	// writing durable versioned notes and briefing the overseer.
	startAmbientResearch(ctx, cfg, mcpSrv, capacityGate(capGov))

	// 🎯T357 periodic full-scan audit: bounded advanced-model passes over
	// code, skills, and prompts, folded into durable residue.
	startPeriodicAudit(ctx, cfg, mcpSrv, capacityGate(capGov))

	// Process-as-cache GC: periodically stop idle spawned threads'
	// processes (resumably) to free resources. The threads persist and
	// rehydrate on the next Direct. Stream-feeds coach (+ residual mint).
	go reapIdleThreads(ctx, btlr, rsiLoop, rsiCoach)

	// Transcript memory is now provided by the standalone mnemo MCP server.
	// See https://github.com/marcelocantos/mnemo

	// Overseer: claudia Session (default backend from config/env; usually Grok).
	overseerModelChoice := cfg.OverseerModel
	if overseerModelChoice == "" {
		overseerModelChoice = cfg.Model
	}
	jevonDef, err := registry.EnsureAgent(cfg.OverseerName, jevDir, overseerModelChoice, true)
	if err != nil {
		slog.Error("jevon agent setup failed", "err", err)
		os.Exit(1)
	}
	// 🎯T148: keep stored provider on resume; only set default when empty.
	jevonDef.Provider = cli.SelectAgentProvider("", jevonDef.Provider, defaultProvider)
	// 🎯T114: overseer purpose on the unified participant record.
	if jevonDef.Purpose == "" {
		jevonDef.Purpose = claudia.PurposeOverseer
	}

	// Overseer MCP registration happens after the listener is bound, so the
	// advertised URL names the port actually served (🎯T379). See below.
	if err := registry.Register(*jevonDef); err != nil {
		slog.Error("jevon agent register failed", "err", err)
		os.Exit(1)
	}
	slog.Info("jevon agent", "provider", jevonDef.Provider, "session", jevonDef.SessionID, "resume", jevonDef.Materialized)

	srv.SetRegistry(registry)
	// 🎯T275: HTTP POST /api/agents/{name}/send uses the same deliver path as
	// MCP jevons_agent_send — queue when busy (not 409 dead-end). Drain on
	// terminal stop is wired in mcpserver agentEventSink (🎯T111.1).
	srv.SetAgentSendHook(func(name, text string) (string, error) {
		res, err := mcpSrv.DeliverAgentMessage(name, text, false)
		if err != nil {
			return "", err
		}
		return res.Status, nil
	})
	// 🎯T309.3: the overseer arm of the fleet's single deliver-by-name path.
	// Without this, an overseer-addressed send from the fleet layer (a worker
	// reply, a PO reporting up, jevons_agent_send by name) would reach the
	// overseer's process directly and bypass the owner chat journal and the
	// notify queue — the 🎯T62 drop. Delivery itself stays implemented once,
	// in the HTTP server, which owns those semantics.
	mcpSrv.SetOverseerDeliver(func(text string, origin mcpserver.SendOrigin) error {
		return srv.DeliverToOverseerAs(text, string(origin))
	})
	// 🎯T82: event-driven fleet panel refresh when agents.json mutates.
	srv.WatchAgentsFile(registryPath)

	listenAddr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)

	// Build the handler, optionally wrapping with mTLS middleware.
	// 🎯T385: the cross-site guard wraps the whole mux, so route groups
	// mounted outside internal/server — the MCP endpoint (which exposes
	// jevons_writ_exec) and the dev server's static routes — are covered on
	// the same terms as the server's own routes.
	var handler http.Handler = server.GuardCrossSite(mux)
	if *enableTLS {
		handler = server.ClientCertMiddleware(handler, "/health", "/api/provision")
	}
	httpSrv := &http.Server{Addr: listenAddr, Handler: handler}

	// Start HTTP server before agents so the MCP endpoint is reachable.
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	}

	// Overseer MCP tools: user-scoped install for the selected overseer
	// provider (🎯T58 Grok, 🎯T212 Claude), registered before launch so tools
	// are present on session/new and session/load (resume keeps tools).
	// Default fleet/overseer remains Grok unless provider is explicitly
	// claude (or other); this does not flip the compile-time default.
	//
	// 🎯T379: this must come *after* the bind, and must advertise
	// servedPort(ln) rather than cfg.Port. Registering first meant that a
	// daemon which then failed to listen — the port already held by another
	// instance — exited having just written a registration for an endpoint
	// it never served, and every agent inheriting that entry waited on it
	// forever. Deriving the URL from the live listener makes that drift
	// unrepresentable, and also handles cfg.Port == 0 (kernel-assigned).
	if err := ensureOverseerMCPServer(cfg, mcpHost, servedPort(ln.Addr()), jevonDef.Provider); err != nil {
		slog.Warn("could not register overseer MCP server — overseer may start toolless",
			"provider", jevonDef.Provider, "err", err)
	}

	if *enableTLS {
		tlsHosts := []string{"localhost", "127.0.0.1", qr.LanIP()}
		tlsCfg, err := ca.TLSConfig(tlsHosts)
		if err != nil {
			slog.Error("cannot build TLS config", "err", err)
			os.Exit(1)
		}
		// Allow optional client certs so the provision endpoint is reachable
		// before a device has a cert. The ClientCertMiddleware enforces the
		// requirement on all other routes.
		tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		ln = tls.NewListener(ln, tlsCfg)
		slog.Info("mTLS enabled", "hosts", tlsHosts)
	}

	go func() {
		if err := httpSrv.Serve(ln); err != http.ErrServerClosed {
			slog.Error("server failed", "err", err)
		}
	}()

	// Now start agents — MCP server is reachable.
	// StartAll resumes session_ids from agents.json; with Grok connect-mode
	// (CLAUDIA_GROK_CONNECT) it reattaches to still-running serve PIDs when
	// ConnectURL/ConnectPID are present (🎯T40 process durability).
	registry.StartAll()

	// Exit policy: normal → StopAll; upgrade (SIGHUP / JEVONS_UPGRADE_EXIT) → leave
	// agents alone and write handles. See docs/design/upgrade-without-drain.md.
	// Written from the signal goroutine; read from defer — use atomic.
	var exitUpgrade atomic.Bool
	defer func() {
		mode := upgrade.ModeNormal
		if exitUpgrade.Load() {
			mode = upgrade.ModeUpgrade
		}
		if mode.StopAgents() {
			registry.StopAll()
			return
		}
		handles := upgrade.FromRegistry(registry)
		snap := upgrade.BuildSnapshot(handles, os.Getpid())
		path := upgrade.SnapshotPath(cfg.StateDir)
		if err := upgrade.SaveSnapshot(path, snap); err != nil {
			slog.Error("upgrade handles save failed", "path", path, "err", err)
		} else {
			slog.Info("upgrade exit: skipped StopAll; wrote agent handles",
				"path", path, "agents", len(handles), "residual", snap.Residual)
		}
	}()

	if jevonProc := registry.Get(cfg.OverseerName); jevonProc != nil {
		// AttachOverseer sets the process and subscribes its event stream
		// (forward raw JSONL to chat WS clients; accumulate turn text +
		// status; feed the Grok voice bridge). It is re-run after a rewind
		// swaps the process, so all overseer references stay current.
		srv.AttachOverseer(jevonProc)

		// A daemon that died between rotating the overseer's row and seeding
		// its successor leaves the handover on disk; the process is alive at
		// boot, so converge sees nothing wrong and this is the only retry
		// (🎯T285). No-op when nothing is pending.
		srv.ResumePendingHandover()

		// Wire MCP worker-completion + agent-reply notifications into Jevon.
		// Route through the server so a rewind swap is transparent.
		send := func(text string) {
			if err := srv.SendToOverseer(text); err != nil {
				slog.Error("notify jevon failed", "err", err)
			}
		}
		mcpSrv.SetNotify(send)

		// 🎯T118: ACP progress / tool steps → RHS fleet-row status chrome.
		// agents_changed only when the glanceable summary changes.
		// 🎯T209: same hook multiplexes inspect live frames on /ws/chat for
		// subscribed fleet agents (primary inspect path; not HTTP poll).
		mcpSrv.SetAgentEventHook(func(name string, ev claudia.Event) {
			if srv.ObserveAgentProgress(name, ev) {
				srv.NotifyAgentsChanged()
			}
			srv.DeliverInspectLive(name, ev)
		})

		// Wire completion-notify for workers auto-started by StartAll above
		// (🎯T61): unlike MCP-tool spawns, they never passed through
		// handleAgentStart, so without this a resumed worker's reply would
		// never reach the overseer.
		mcpSrv.WireRunningAgents(cfg.OverseerName)

		// No recap needed: the overseer resumes its real session on
		// restart (via config.toml MCP tools that survive session/load,
		// 🎯T58), so the model already has full context. The recap-on-boot
		// hack — a lossy summary re-injected as a visible user turn every
		// restart — is gone.
	} else {
		// The overseer did not launch (StartAll logged "auto-start failed"),
		// so the chat cannot respond. Fail LOUD and legible with
		// provider-aware diagnosis (🎯T54, 🎯T214 J4) — do not blame a missing
		// Grok CLI when the overseer provider is Claude/other. Cockpit
		// converge (🎯T204) keeps retrying Launch+attach.
		reason := overseerUnavailableReason(jevonDef.Provider)
		slog.Error("OVERSEER NOT RUNNING — chat cannot respond until this is fixed",
			"overseer", cfg.OverseerName, "provider", jevonDef.Provider, "likely_cause", reason)
		srv.SetOverseerDownReason(reason)
	}

	// 🎯T335: standing security auditor — always on; deliver via overseer when attached.
	secInterest := mcpserver.NewSecurityInterest(mcpSrv, func(text string) error {
		if err := srv.SendToOverseer(text); err != nil {
			slog.Error("security auditor notify failed", "err", err)
			return err
		}
		return nil
	})
	mcpSrv.SetSecurityAuditor(secInterest)
	srv.SetSecurityAuditor(secInterest)
	slog.Info("security auditor ready", "name", "security-auditor", "writ", writAvail)

	// 🎯T317 impatience ladder (T316 set + T318 attenuation + sinks).
	// Constructed at the SetIdlePressureHooks seam (2a065e5): RePressure →
	// idle-nudge deliver, Overseer → event_push to root, Human → sticky OS
	// alert (39802c7). Unwired sticky fails construction, not fire.
	var humanSink converge.HumanSink
	if be, err := sticky.NewOSNotifier(); err != nil {
		slog.Warn("impatience sticky unwired — human rung will error until terminal-notifier is available",
			"err", err)
	} else if st := sticky.New(be); st != nil {
		humanSink = st
		// Withdraw every raised alert on shutdown so a stale "stuck" does not
		// outlive the daemon that could have cleared it (🎯T319).
		defer func() {
			for _, err := range st.ClearAll() {
				slog.Warn("impatience sticky clear-all", "err", err)
			}
		}()
	}
	overseerName := cfg.OverseerName
	if overseerName == "" {
		overseerName = "jevons"
	}
	mcpSrv.SetImpatienceEngine(mcpserver.NewImpatienceEngine(mcpserver.ImpatienceEngineArgs{
		Sinks:      mcpserver.NewImpatienceSinks(mcpSrv, overseerName, humanSink),
		Postmortem: server.NewPostmortemSink(srv), // 🎯T319: closed incident → root report
		Overseer:   overseerName,
	}))
	// MissionOpen from the nearest bullseye ledger for each bound target
	// (workdir of an engaged agent). Unknown ledger row stays open (residual).
	mcpSrv.SetIdlePressureHooks(mcpserver.IdlePressureHooks{
		MissionOpen: func(targetID string) bool {
			tid := strings.TrimSpace(strings.TrimPrefix(targetID, "🎯"))
			if tid == "" {
				return true
			}
			for _, d := range registry.List() {
				if strings.TrimSpace(strings.TrimPrefix(d.TargetID, "🎯")) != tid {
					continue
				}
				st, ok := targetfile.LoadTargetStatusFromCwd(d.WorkDir, tid)
				if !ok {
					return true
				}
				return targetfile.IsOpenStatus(st)
			}
			return true
		},
		// 🎯T415: convergence gave up. The notice inside is deterministic
		// and depends on no agent; the recovery agent it also dispatches
		// is allowed to fail, including failing to spawn.
		OnMaxed: mcpSrv.OnConvergenceExhausted,
	})

	// 🎯T415: the owner-notice path writes straight to the owner's journal.
	// Deliberately NOT via the overseer, unlike every other fleet-health
	// signal: any failure that stops the overseer would otherwise stop the
	// report of it, which is how the 2026-08-10 outage stayed invisible for
	// hours while the sentinel logged overseer:down with outcome=ok.
	mcpSrv.SetOwnerNotifier(mcpserver.NewOwnerNotifier(srv.NotifyOwnerNote))

	// 🎯T171 dual-path: daemon-restarted → POs+overseer; short resume →
	// open-mission workers (T207 brief-or-verify); worker-idle transition → PO.
	// Also wires fleet-health hook for cockpit (T204).
	// idlePressureSweep (via this loop) drives the T317 ladder when installed.
	go mcpserver.StartIdleNudgeLoop(ctx, mcpserver.IdleNudgeLoopArgs{
		Server:       mcpSrv,
		StateDir:     cfg.StateDir,
		OverseerName: cfg.OverseerName,
		DefaultPO:    "jevons-po",
	})

	// 🎯T254.1: unattended frontier consumption — every non-design-gated ready
	// frontier leaf gets a worker under jevons-po (or an explicit park reason)
	// without an owner spawn command. Conservative drip (default 1 spawn per
	// 10m cycle), durable per-target backoff/max, budget clamp composed.
	if cfg.FrontierConsume.Disabled {
		slog.Info("frontier consume loop disabled by config (🎯T254.1)")
	} else {
		go mcpserver.StartFrontierConsumeLoop(ctx, mcpserver.FrontierConsumeLoopArgs{
			Server:             mcpSrv,
			StateDir:           cfg.StateDir,
			Workdir:            cfg.WorkDir,
			ParentPO:           "jevons-po",
			Interval:           time.Duration(cfg.FrontierConsume.IntervalMinutes) * time.Minute,
			MaxSpawnsPerCycle:  cfg.FrontierConsume.MaxSpawnsPerCycle,
			MaxSpawnsPerTarget: cfg.FrontierConsume.MaxSpawnsPerTarget,
		})
	}

	// 🎯T204: cockpit converge — overseer Alive+Attach+turn-usable; fleet
	// dead-handle recovery. Restart dual-path is T171 (not periodic ladder).
	srv.SetCockpitHooks(server.CockpitHooks{
		FleetHealth: func() { mcpSrv.SweepFleetHealth(cfg.OverseerName) },
		FleetNudge:  func() { mcpSrv.TriggerIdleNudgeSweep() }, // health only
	})
	srv.StartCockpitConverge(ctx, server.DefaultCockpitInterval)

	// 🎯T219: durable sentinel — continuous observe→classify→act while up.
	// Pure watcher: control-plane repair / file+PO mission; no product implement; no Ship.
	// Complements T204/T207 mechanical floor and T325.4 one-shot staff ops.
	go mcpserver.StartSentinelLoop(ctx, mcpserver.SentinelLoopArgs{
		Server:    mcpSrv,
		StateDir:  cfg.StateDir,
		Workdir:   cfg.WorkDir,
		Overseer:  cfg.OverseerName,
		DefaultPO: "jevons-po",
	})

	// 🎯T50/🎯T54 regression oracle, hoisted out of the attach block so it
	// fires even when the overseer never launched: if no MCP client has
	// listed our tools shortly after boot, the overseer is toolless or
	// absent. Fail LOUD.
	go func() {
		time.Sleep(45 * time.Second)
		if mcpSrv.ToolsListCount() == 0 {
			slog.Error("OVERSEER TOOLLESS OR ABSENT — no MCP tools/list since boot; jevons tools are not attached (see 🎯T50/🎯T54)")
			srv.Broadcast(map[string]any{
				"type":  "error",
				"error": "overseer has no jevons tools (or failed to launch) — actions will not work",
			})
		}
	}()

	// Graceful shutdown on signal (🎯T40: SIGHUP → upgrade exit).
	go func() {
		sig := <-sigCh
		if sig == syscall.SIGHUP || upgrade.EnvRequestsUpgrade() {
			exitUpgrade.Store(true)
		}
		mode := upgrade.ModeNormal
		if exitUpgrade.Load() {
			mode = upgrade.ModeUpgrade
		}
		slog.Info("shutting down", "signal", sig, "exit_mode", mode.String(),
			"stop_agents", mode.StopAgents())
		cancel()
		httpSrv.Close()
	}()

	slog.Info("jevonsd starting", "addr", listenAddr, "version", cli.Version,
		"worker_model", cfg.Model)

	// Connect to relay if specified.
	if *relayURL != "" {
		token := *relayToken
		if token == "" {
			token = os.Getenv("TERN_TOKEN")
		}
		instanceID, err := srv.ConnectRelay(ctx, *relayURL, token, *relayInstanceID)
		if err != nil {
			slog.Error("relay connection failed", "err", err)
			os.Exit(1)
		}
		slog.Info("relay registered", "instance_id", instanceID, "credential_loaded", srv.Credentials().Get() != nil)
	} else if srv.Credentials().Get() == nil {
		slog.Info("no credential and no relay configured — pair a device with `jevonsd --pair <instance-id> --relay <url>`")
	}

	// Block until shutdown signal.
	<-ctx.Done()
}

// runOneShot handles --pair and --add-credential flags. Both modes
// load (or create) the credential store and exit; they do not start
// the daemon.
func runOneShot(pairInstance, addCredential, relayURL string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home directory:", err)
		os.Exit(1)
	}
	store := server.NewCredentialStore(filepath.Join(homeDir, ".jevons", "credential.json"))
	if _, err := store.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "load credential:", err)
		os.Exit(1)
	}

	if addCredential != "" {
		data, err := os.ReadFile(addCredential)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read server record:", err)
			os.Exit(1)
		}
		var rec crypto.PairingRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			fmt.Fprintln(os.Stderr, "parse server record:", err)
			os.Exit(1)
		}
		if err := store.Save(&rec); err != nil {
			fmt.Fprintln(os.Stderr, "save credential:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "credential saved for peer %s\n", rec.PeerInstanceID)
		return
	}

	// --pair mode.
	if relayURL == "" {
		fmt.Fprintln(os.Stderr, "--pair requires --relay")
		os.Exit(2)
	}
	host := pigeon.NewPairingHost(relayURL)
	artifact, serverRec, err := host.Mint(pairInstance)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mint:", err)
		os.Exit(1)
	}
	if err := store.Save(serverRec); err != nil {
		fmt.Fprintln(os.Stderr, "save server record:", err)
		os.Exit(1)
	}
	text, err := artifact.MarshalText()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal artifact:", err)
		os.Exit(1)
	}
	qr.Print(os.Stderr, string(text))
	fmt.Println(string(text))
	fmt.Fprintf(os.Stderr, "artifact for %s expires %s\n", pairInstance, artifact.ExpiresAt.Format(time.RFC3339))
}

// keyInstructions maps service names to instructions for obtaining the key.
var keyInstructions = map[string]string{
	"xai-api-key": `To get an xAI API key:
  1. Go to https://console.x.ai/
  2. Sign in with your X (Twitter) account
  3. Navigate to API Keys
  4. Click "Create API Key"
  5. Copy the key and paste it below
`,
	"openai-api-key": `To get an OpenAI API key:
  1. Go to https://platform.openai.com/api-keys
  2. Sign in or create an account
  3. Click "Create new secret key"
  4. Copy the key and paste it below
`,
}

// promptAndStoreKey prompts for a key with hidden input, stores it, and exits.
func promptAndStoreKey(label, service string) {
	if instructions, ok := keyInstructions[service]; ok {
		fmt.Fprint(os.Stderr, instructions)
	}
	fmt.Fprintf(os.Stderr, "%s key: ", label)
	key, err := readSecret()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nFailed to read key: %v\n", err)
		os.Exit(1)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		fmt.Fprintln(os.Stderr, "\nNo key entered.")
		os.Exit(1)
	}
	if err := storeKeychainKey(service, key); err != nil {
		fmt.Fprintf(os.Stderr, "\nFailed to store key: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\n%s key stored in macOS Keychain.\n", label)
	os.Exit(0)
}

// readSecret reads a line from stdin with echo disabled.
func readSecret() (string, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		// Fallback: not a terminal, just read a line.
		var line string
		_, err := fmt.Scanln(&line)
		return line, err
	}
	defer term.Restore(fd, old)

	var buf []byte
	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			return string(buf), err
		}
		if b[0] == '\n' || b[0] == '\r' {
			return string(buf), nil
		}
		if b[0] == 3 { // Ctrl-C
			return "", fmt.Errorf("interrupted")
		}
		if b[0] == 127 || b[0] == 8 { // Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(os.Stderr, "\b \b")
			}
			continue
		}
		buf = append(buf, b[0])
		fmt.Fprint(os.Stderr, "*")
	}
}

// storeKeychainKey stores a value in the macOS Keychain under the "jevons" account.
func storeKeychainKey(service, value string) error {
	// Delete any existing entry first (add fails if it exists).
	exec.Command("security", "delete-generic-password",
		"-a", "jevons", "-s", service).Run()
	return exec.Command("security", "add-generic-password",
		"-a", "jevons", "-s", service, "-w", value).Run()
}

// loadKeychainKey retrieves a value from the macOS Keychain.
func loadKeychainKey(service string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", "jevons", "-s", service, "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// grokCandidatePaths lists where the Grok CLI is commonly installed, in
// preference order, for the fallback when it is not on PATH.
func grokCandidatePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".grok", "bin", "grok"),
		filepath.Join(home, ".local", "bin", "grok"),
		"/opt/homebrew/bin/grok", "/usr/local/bin/grok",
	}
}

// grokBin resolves the Grok CLI executable: PATH first, then the known
// install locations. Returns "grok" as a last resort so exec produces a
// clear "executable not found" error rather than a silent no-op.
func grokBin() string {
	if p, err := exec.LookPath("grok"); err == nil {
		return p
	}
	for _, p := range grokCandidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return "grok"
}

// overseerMCPKind names which CLI path installs overseer MCP tools (🎯T212).
type overseerMCPKind string

const (
	overseerMCPGrok   overseerMCPKind = "grok"
	overseerMCPClaude overseerMCPKind = "claude"
	overseerMCPNone   overseerMCPKind = "none"
)

// overseerMCPKindForProvider is the pure oracle for 🎯T212: which user-scoped
// MCP ensure path the boot/attach code must take. Default/empty and Grok →
// Grok config.toml; Claude → claude mcp -s user; Codex/Bedrock → none
// (accepted residual: no known durable user-scope MCP install CLI).
func overseerMCPKindForProvider(provider claudia.Provider) overseerMCPKind {
	switch provider {
	case claudia.ProviderClaude:
		return overseerMCPClaude
	case claudia.ProviderCodex, claudia.ProviderBedrock:
		return overseerMCPNone
	default:
		// Grok and unknown/empty: keep historical Grok path (default fleet).
		return overseerMCPGrok
	}
}

// ensureOverseerMCPServer registers jevonsd's MCP endpoint for the selected
// overseer provider before launch (🎯T58, 🎯T212). Grok writes user-scoped
// ~/.grok/config.toml; Claude writes user-scoped Claude MCP config via
// `claude mcp add -s user`. Idempotent remove-then-add so port/bind changes
// always land. Residual: codex/bedrock return a clear skip error (class-3
// until a durable install path exists); live Claude smoke remains optional.
func ensureOverseerMCPServer(cfg config.Config, host string, port int, provider claudia.Provider) error {
	switch overseerMCPKindForProvider(provider) {
	case overseerMCPClaude:
		return ensureClaudeMCPServer(cfg, host, port)
	case overseerMCPNone:
		return fmt.Errorf("overseer provider %q has no user-scoped MCP ensure path yet (residual; overseer may start toolless)", provider)
	default:
		return ensureGrokMCPServer(cfg, host, port)
	}
}

// ensureGrokMCPServer registers jevonsd's MCP endpoint user-scoped in
// ~/.grok/config.toml so the Grok CLI attaches it on BOTH session/new and
// session/load — the key to the overseer resuming its real session across
// restarts (🎯T58) instead of the old rotate-and-recap hack. Idempotent:
// removes any prior entry (ignoring the not-found error) then re-adds the
// current URL, so a changed port/bind is always reflected.
func ensureGrokMCPServer(cfg config.Config, host string, port int) error {
	name, url := overseerMCPServerSpec(cfg, host, port)
	grok := grokBin()
	args := grokMCPAddArgs(name, url)
	_ = exec.Command(grok, "mcp", "remove", name).Run()
	out, err := exec.Command(grok, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("grok mcp add %s %s: %w (%s)", name, url, err, strings.TrimSpace(string(out)))
	}
	slog.Info("registered overseer MCP server (Grok user-scoped, survives session/load)",
		"name", name, "url", url, "provider", "grok")
	return nil
}

// ensureClaudeMCPServer registers jevonsd's MCP endpoint user-scoped in
// Claude Code config (`claude mcp add --transport http -s user`) so a
// Claude overseer gets jevons tools on session start and resume (🎯T212).
// Idempotent remove-then-add. Residual: live Claude overseer smoke is
// class-3/owner-gated; hermetics cover kind routing + argv shape.
func ensureClaudeMCPServer(cfg config.Config, host string, port int) error {
	name, url := overseerMCPServerSpec(cfg, host, port)
	bin := claudeBin()
	args := claudeMCPAddArgs(name, url)
	_ = exec.Command(bin, "mcp", "remove", "-s", "user", name).Run()
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude mcp add %s %s: %w (%s)", name, url, err, strings.TrimSpace(string(out)))
	}
	slog.Info("registered overseer MCP server (Claude user-scoped)",
		"name", name, "url", url, "provider", "claude")
	return nil
}

// overseerMCPServerSpec derives the MCP registration name and streamable-HTTP
// endpoint URL for the overseer. The path must be exactly /mcp (mcpserver
// mount) and the host a concrete address — never "localhost" (::1 vs
// 127.0.0.1; 🎯T6/🎯T58). Shared by Grok and Claude ensure paths (🎯T212).
//
// port is the port the daemon actually serves, not the configured one
// (🎯T379). Those differ whenever the listen fails or the config asks for an
// ephemeral port, and a registration written from the intended value in
// either case advertises an endpoint no process is behind — which is the
// silent-dead-registration failure this target exists to prevent.
func overseerMCPServerSpec(cfg config.Config, host string, port int) (name, url string) {
	return cfg.MCPServerName, fmt.Sprintf("http://%s:%d/mcp", host, port)
}

// servedPort reports the TCP port a bound listener actually occupies. It is
// the only honest source for the advertised URL: cfg.Port may be 0
// (ephemeral, resolved by the kernel) and, until the bind succeeds, may be a
// port another process holds.
func servedPort(addr net.Addr) int {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return tcp.Port
	}
	return 0
}

// grokMCPAddArgs is the pure argv for `grok mcp add` after the binary
// (oracle: transport http, name, concrete /mcp URL).
func grokMCPAddArgs(name, url string) []string {
	return []string{"mcp", "add", "--transport", "http", name, url}
}

// claudeMCPAddArgs is the pure argv for `claude mcp add` after the binary
// (🎯T212 oracle: user scope so tools survive resume; transport http).
func claudeMCPAddArgs(name, url string) []string {
	return []string{"mcp", "add", "--transport", "http", "-s", "user", name, url}
}

// claudeCandidatePaths lists common Claude Code install locations when the
// binary is not on PATH (symlink often under ~/.local/bin).
func claudeCandidatePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		"/opt/homebrew/bin/claude", "/usr/local/bin/claude",
	}
}

// claudeBin resolves the Claude CLI: PATH first, then known install paths.
// Returns "claude" as last resort so exec fails loudly if missing.
func claudeBin() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	for _, p := range claudeCandidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return "claude"
}

// diagnoseOverseerUnavailable is the pure oracle for 🎯T214 J4: message text
// is keyed by overseer provider. Missing Grok connect fields must not
// mislabel a Claude/Codex/Bedrock overseer as "Grok is broken".
//
// binOnPath: provider CLI found on PATH. candidatePath: non-empty when a
// known install path exists off-PATH (Grok only today).
func diagnoseOverseerUnavailable(provider claudia.Provider, binOnPath bool, candidatePath string) string {
	switch provider {
	case claudia.ProviderClaude:
		if binOnPath {
			return "the Claude agent failed to start — check that the Claude CLI is authenticated " +
				"and see the jevonsd log for details (overseer provider is claude)"
		}
		return "the Claude CLI is not installed (or not on PATH) — install Claude Code, authenticate, " +
			"then restart jevonsd (overseer provider is claude; this is not a Grok CLI issue)"
	case claudia.ProviderCodex:
		if binOnPath {
			return "the Codex agent failed to start — check Codex auth and see the jevonsd log " +
				"(overseer provider is codex)"
		}
		return "the Codex CLI is not installed (or not on PATH) — install Codex, authenticate, " +
			"then restart jevonsd (overseer provider is codex; this is not a Grok CLI issue)"
	case claudia.ProviderBedrock:
		return "the Bedrock agent failed to start — check AWS credentials/region and see the " +
			"jevonsd log (overseer provider is bedrock; this is not a Grok CLI issue)"
	default:
		// Grok (default fleet) and unknown providers: Grok-oriented copy.
		if binOnPath {
			return "the Grok agent failed to start — check that the Grok CLI is signed in " +
				"(`grok login`, or set XAI_API_KEY) and see the jevonsd log for details"
		}
		if candidatePath != "" {
			return "the Grok agent failed to start — check that the Grok CLI at " + candidatePath +
				" is signed in (`grok login`, or set XAI_API_KEY)"
		}
		return "the Grok CLI is not installed — install Grok Build and sign in " +
			"(`grok login`, or set XAI_API_KEY), then restart jevonsd. The default overseer " +
			"provider is Grok; set provider=claude (or config/env) only when that backend is ready"
	}
}

// overseerUnavailableReason probes the host for the selected overseer
// provider's CLI and returns a legible, actionable explanation (🎯T54, 🎯T214).
func overseerUnavailableReason(provider claudia.Provider) string {
	switch provider {
	case claudia.ProviderClaude:
		if _, err := exec.LookPath("claude"); err == nil {
			return diagnoseOverseerUnavailable(provider, true, "")
		}
		for _, p := range claudeCandidatePaths() {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return diagnoseOverseerUnavailable(provider, true, "")
			}
		}
		return diagnoseOverseerUnavailable(provider, false, "")
	case claudia.ProviderCodex:
		_, err := exec.LookPath("codex")
		return diagnoseOverseerUnavailable(provider, err == nil, "")
	case claudia.ProviderBedrock:
		return diagnoseOverseerUnavailable(provider, false, "")
	default:
		if _, err := exec.LookPath("grok"); err == nil {
			return diagnoseOverseerUnavailable(claudia.ProviderGrok, true, "")
		}
		for _, p := range grokCandidatePaths() {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return diagnoseOverseerUnavailable(claudia.ProviderGrok, false, p)
			}
		}
		return diagnoseOverseerUnavailable(claudia.ProviderGrok, false, "")
	}
}

// reapIdleGCInterval is how often the butler sweeps for idle spawned
// threads whose processes can be stopped resumably.
const reapIdleGCInterval = 2 * time.Minute

// reapIdleThreads runs the process-as-cache GC sweep until ctx is done.
// Reaped thread ids stream-feed the RSI coach (🎯T243) and residual mint loop (🎯T92).
func reapIdleThreads(ctx context.Context, btlr *butler.Butler, rsiLoop *rsi.Loop, rsiCoach *rsi.Coach) {
	ticker := time.NewTicker(reapIdleGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reaped := btlr.ReapIdle(); len(reaped) > 0 {
				slog.Info("reaped idle thread processes", "threads", reaped)
				if rsiCoach != nil {
					rsiCoach.NoteReaped(reaped)
				}
				if rsiLoop != nil {
					rsiLoop.NoteReaped(reaped)
				}
			}
		}
	}
}

// startAmbientRSICoach wires the 🎯T243 product path: drip-read surfaces →
// structured judgments → overseer (never bullseye file).
// Env:
//
//	JEVONS_RSI_COACH=0       — disable coach schedule (MCP tools still register if started)
//	JEVONS_RSI_COACH_INTERVAL — duration (default 15m); "0" disables ticker
//	JEVONS_RSI_NO_CHAT       — "1" skip owner-chatlog surface
//	JEVONS_RSI_NO_SESSION    — "1" skip session-transcript surface
func startAmbientRSICoach(ctx context.Context, cfg config.Config, mcpSrv *mcpserver.Server, gate capacity.Gate) *rsi.Coach {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("JEVONS_RSI_COACH"))) {
	case "0", "false", "no", "off":
		slog.Info("ambient RSI coach disabled by JEVONS_RSI_COACH")
		return nil
	}
	interval := rsi.DefaultCoachInterval
	if v := strings.TrimSpace(os.Getenv("JEVONS_RSI_COACH_INTERVAL")); v != "" {
		if v == "0" {
			interval = -1
		} else if d, err := time.ParseDuration(v); err == nil {
			interval = d
		} else {
			slog.Warn("JEVONS_RSI_COACH_INTERVAL invalid; using default", "value", v, "default", rsi.DefaultCoachInterval)
		}
	} else if v := strings.TrimSpace(os.Getenv("JEVONS_RSI_INTERVAL")); v != "" {
		// Shared interval env still honoured for coach when coach-specific unset.
		if v == "0" {
			interval = -1
		} else if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	chatLogPath := ""
	if !envTruthy("JEVONS_RSI_NO_CHAT") {
		name := cfg.OverseerName
		if name == "" {
			name = "jevons"
		}
		chatLogPath = filepath.Join(cfg.StateDir, "chatlog", name+".jsonl")
	}
	sessionsDir := ""
	if !envTruthy("JEVONS_RSI_NO_SESSION") {
		sessionsDir = strings.TrimSpace(cfg.SessionsDir)
	}
	// Retrospective history mine (🎯T353): the drip starts at EOF, so past
	// evidence is reached by bounded backward passes over this repo's git
	// history plus the journal tails.
	retroWorkdir := strings.TrimSpace(os.Getenv("JEVONS_RSI_RETRO_WORKDIR"))
	if retroWorkdir == "" {
		retroWorkdir = strings.TrimSpace(cfg.WorkDir)
	}
	deliverer := mcpSrv.NewOverseerJudgmentDeliverer(cfg.OverseerName)
	coach, err := rsi.NewCoach(rsi.CoachArgs{
		StateDir:     cfg.StateDir,
		Capacity:     gate,
		ChatLogPath:  chatLogPath,
		SessionsDir:  sessionsDir,
		Interval:     interval,
		Deliverer:    deliverer,
		SeedEOF:      true, // continuous drip: only new appends after boot
		RetroWorkdir: retroWorkdir,
		DryRun:       false,
	})
	if err != nil {
		slog.Warn("ambient RSI coach disabled", "err", err)
		return nil
	}
	// Ensure durable default config exists for overseer retune surface.
	if _, err := os.Stat(rsi.CoachConfigPath(cfg.StateDir)); os.IsNotExist(err) {
		def := rsi.DefaultCoachConfig()
		if name := strings.TrimSpace(cfg.OverseerName); name != "" {
			def.Overseer = name
		}
		_ = rsi.SaveCoachConfig(cfg.StateDir, def)
	}
	mcpSrv.SetRSICoach(coach)
	go coach.Run(ctx)
	slog.Info("ambient RSI coach ready",
		"interval", interval.String(),
		"chatlog", chatLogPath != "",
		"sessions", sessionsDir != "",
		"retro_workdir", retroWorkdir,
		"retro_interval", rsi.DefaultRetroInterval.String(),
		"overseer", cfg.OverseerName,
		"config", "state_dir/rsi/coach_config.json",
		"cursor", "state_dir/rsi/coach_cursor.json",
		"ledger", "state_dir/rsi/judged.json",
	)
	return coach
}

// startAmbientResearch wires the 🎯T356 standing research cycle: periodic
// exploration of the surrounding context (this repo, sibling repos in the same
// org root, the frontier, the eventlog, sessions) plus an async feed trigger,
// folded into durable versioned notes under state_dir/research.
//
// Feeds ship off: the owner subscribes explicitly with
// jevons_research_configure, and only subscribed hosts are ever fetched.
//
// Env:
//
//	JEVONS_RESEARCH=0        — disable the schedule (MCP tools stay registered)
//	JEVONS_RESEARCH_INTERVAL — duration (default 90m); "0" disables the ticker
func startAmbientResearch(ctx context.Context, cfg config.Config, mcpSrv *mcpserver.Server, gate capacity.Gate) *research.Agent {
	interval := time.Duration(0) // 0 = follow durable config
	if v := strings.TrimSpace(os.Getenv("JEVONS_RESEARCH_INTERVAL")); v != "" {
		if v == "0" {
			interval = -1
		} else if d, err := time.ParseDuration(v); err == nil {
			interval = d
		} else {
			slog.Warn("JEVONS_RESEARCH_INTERVAL invalid; using config", "value", v)
		}
	}
	// Sibling repositories live beside this one under the same org root.
	var relatedRoots []string
	if wd := strings.TrimSpace(cfg.WorkDir); wd != "" {
		if abs, err := filepath.Abs(wd); err == nil {
			relatedRoots = append(relatedRoots, filepath.Dir(abs))
		}
	}
	agent, err := research.New(research.Args{
		StateDir:     cfg.StateDir,
		Capacity:     gate,
		Workdir:      cfg.WorkDir,
		RelatedRoots: relatedRoots,
		EventLogPath: eventlog.DefaultPath(cfg.StateDir),
		SessionsDir:  strings.TrimSpace(cfg.SessionsDir),
		Deliverer:    mcpSrv.NewOverseerBriefDeliverer(cfg.OverseerName),
		Interval:     interval,
	})
	if err != nil {
		slog.Warn("ambient research disabled", "err", err)
		return nil
	}
	// Ensure a durable default config exists as the overseer retune surface.
	if _, err := os.Stat(research.ConfigPath(cfg.StateDir)); os.IsNotExist(err) {
		def := research.DefaultConfig()
		if name := strings.TrimSpace(cfg.OverseerName); name != "" {
			def.Overseer = name
		}
		def.Workdir = cfg.WorkDir
		def.RelatedRoots = relatedRoots
		_ = research.SaveConfig(cfg.StateDir, def)
	}
	mcpSrv.SetResearchAgent(agent)
	switch strings.ToLower(strings.TrimSpace(os.Getenv("JEVONS_RESEARCH"))) {
	case "0", "false", "no", "off":
		slog.Info("ambient research schedule disabled by JEVONS_RESEARCH (tools still registered)")
		return agent
	}
	go agent.Run(ctx)
	slog.Info("ambient research ready",
		"workdir", cfg.WorkDir,
		"related_roots", relatedRoots,
		"overseer", cfg.OverseerName,
		"config", "state_dir/research/config.json",
		"notes", "state_dir/research/notes",
	)
	return agent
}

// startPeriodicAudit wires the 🎯T357 standing full-scan audit: a bounded,
// scheduled pass that scopes product code, the skills trees, and agent
// prompts, hands the manifest to an advanced-tier auditor, and folds the
// findings into durable residue under state_dir/audit.
//
// The auditor suggests and the overseer disposes: critical findings notify
// the overseer in the same cycle, but nothing here files a bullseye target
// or rewrites a line of source.
//
// Env:
//
//	JEVONS_AUDIT=0         — disable the schedule (MCP tools stay registered)
//	JEVONS_AUDIT_INTERVAL  — duration (default 24h); "0" disables the ticker
func startPeriodicAudit(ctx context.Context, cfg config.Config, mcpSrv *mcpserver.Server, gate capacity.Gate) *audit.Auditor {
	interval := time.Duration(0) // 0 = follow durable config
	if v := strings.TrimSpace(os.Getenv("JEVONS_AUDIT_INTERVAL")); v != "" {
		if v == "0" {
			interval = -1
		} else if d, err := time.ParseDuration(v); err == nil {
			interval = d
		} else {
			slog.Warn("JEVONS_AUDIT_INTERVAL invalid; using config", "value", v)
		}
	}
	home, _ := os.UserHomeDir()
	auditor, err := audit.New(audit.Args{
		StateDir:  cfg.StateDir,
		Workdir:   cfg.WorkDir,
		Home:      home,
		Runner:    audit.ExecRunner{},
		Deliverer: mcpSrv.NewOverseerAuditDeliverer(cfg.OverseerName),
		Capacity:  gate,
		Interval:  interval,
	})
	if err != nil {
		slog.Warn("periodic audit disabled", "err", err)
		return nil
	}
	// Ensure a durable default config exists as the overseer retune surface.
	// The advanced-tier model pin lives here, not at the call site, so it is
	// inspectable and retunable rather than hardcoded (🎯T357 acceptance #2).
	if _, err := os.Stat(audit.ConfigPath(cfg.StateDir)); os.IsNotExist(err) {
		def := audit.DefaultConfig(cfg.WorkDir, home)
		if name := strings.TrimSpace(cfg.OverseerName); name != "" {
			def.Overseer = name
		}
		if err := audit.SaveConfig(cfg.StateDir, def); err != nil {
			slog.Warn("audit default config save failed", "err", err)
		}
	}
	mcpSrv.SetAuditor(auditor)
	switch strings.ToLower(strings.TrimSpace(os.Getenv("JEVONS_AUDIT"))) {
	case "0", "false", "no", "off":
		slog.Info("periodic audit schedule disabled by JEVONS_AUDIT (tools still registered)")
		return auditor
	}
	go auditor.Run(ctx)
	acfg, _ := auditor.Config()
	slog.Info("periodic audit ready",
		"workdir", cfg.WorkDir,
		"model", acfg.EffectiveModel(),
		"interval", acfg.IntervalDuration(),
		"overseer", acfg.EffectiveOverseer(),
		"config", "state_dir/audit/config.json",
		"residue", "state_dir/audit/residue.json",
		"reports", "state_dir/audit/reports",
	)
	return auditor
}

// startAmbientRSIMint wires residual 🎯T92 direct bullseye mint (NOT product path).
// Product path is the coach (🎯T243). Phrase-list / mint only when explicitly opted in:
//
//	JEVONS_RSI_MINT=1  — enable eventlog mint loop (still subject to DRY_RUN)
//	JEVONS_RSI_DEEPER=1 — also feed chatlog+session phrase extract into mint (noisy)
//	JEVONS_RSI_INTERVAL, JEVONS_RSI_MINT_CWD, JEVONS_RSI_DRY_RUN as before
//
// When neither MINT nor DEEPER is set, the mint loop is not started (coach only).
func startAmbientRSIMint(ctx context.Context, cfg config.Config, mcpSrv *mcpserver.Server) *rsi.Loop {
	mintOn := envTruthy("JEVONS_RSI_MINT")
	deeper := envTruthy("JEVONS_RSI_DEEPER")
	if !mintOn && !deeper {
		slog.Info("ambient RSI mint residual off (product path is coach 🎯T243); set JEVONS_RSI_MINT=1 to enable")
		return nil
	}
	interval := rsi.DefaultInterval
	if v := strings.TrimSpace(os.Getenv("JEVONS_RSI_INTERVAL")); v != "" {
		if v == "0" {
			interval = -1
		} else if d, err := time.ParseDuration(v); err == nil {
			interval = d
		} else {
			slog.Warn("JEVONS_RSI_INTERVAL invalid; using default", "value", v, "default", rsi.DefaultInterval)
		}
	}
	mintCwd := strings.TrimSpace(os.Getenv("JEVONS_RSI_MINT_CWD"))
	if mintCwd == "" {
		mintCwd = resolveRSIMintCwd(cfg)
	}
	dry := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("JEVONS_RSI_DRY_RUN"))) {
	case "1", "true", "yes", "on":
		dry = true
	}
	chatLogPath := ""
	sessionsDir := ""
	if deeper {
		if !envTruthy("JEVONS_RSI_NO_CHAT") {
			chatLogPath = filepath.Join(cfg.StateDir, "chatlog", cfg.OverseerName+".jsonl")
		}
		if !envTruthy("JEVONS_RSI_NO_SESSION") {
			sessionsDir = strings.TrimSpace(cfg.SessionsDir)
		}
	}
	loop, err := rsi.NewLoop(rsi.LoopArgs{
		StateDir:    cfg.StateDir,
		MintCwd:     mintCwd,
		Interval:    interval,
		DryRun:      dry,
		ChatLogPath: chatLogPath,
		SessionsDir: sessionsDir,
	})
	if err != nil {
		slog.Warn("ambient RSI mint residual disabled", "err", err)
		return nil
	}
	mcpSrv.SetRSILoop(loop)
	go loop.Run(ctx)
	slog.Info("ambient RSI mint residual ready (not product path)",
		"interval", interval.String(),
		"mint_cwd", mintCwd,
		"dry_run", dry,
		"deeper_phrase_mint", deeper,
		"chatlog", chatLogPath != "",
		"sessions", sessionsDir != "",
		"ledger", "state_dir/rsi/minted.json",
	)
	return loop
}

func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// resolveRSIMintCwd picks the bullseye repo for ambient mints: product tree
// under ReposRoot when present, else WorkDir / process cwd.
func resolveRSIMintCwd(cfg config.Config) string {
	cand := filepath.Join(cfg.ReposRoot, "marcelocantos", "jevons")
	if st, err := os.Stat(filepath.Join(cand, "bullseye.yaml")); err == nil && !st.IsDir() {
		return cand
	}
	if wd := strings.TrimSpace(cfg.WorkDir); wd != "" && wd != "." {
		if st, err := os.Stat(filepath.Join(wd, "bullseye.yaml")); err == nil && !st.IsDir() {
			return wd
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return cfg.WorkDir
}
