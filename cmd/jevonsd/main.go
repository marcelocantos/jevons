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
	"github.com/marcelocantos/jevons/internal/auth"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/doit"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
	"github.com/marcelocantos/jevons/internal/upgrade"
	"github.com/marcelocantos/jevons/internal/workers"

	"github.com/marcelocantos/pigeon"
	"github.com/marcelocantos/pigeon/crypto"
	"github.com/marcelocantos/pigeon/qr"
)

func main() {
	configPath := flag.String("config", "", "config file (default ~/.jevons/config.yaml; missing file = built-in defaults)")
	port := flag.Int("port", 13705, "listen port")
	bindAddr := flag.String("bind", "", "listen interface (default 127.0.0.1 — loopback only; remote devices use the pigeon relay)")
	relayURL := flag.String("relay", "", "relay URL to register with (e.g. wss://tern.fly.dev)")
	relayToken := flag.String("relay-token", "", "bearer token for relay authentication (or set TERN_TOKEN env var)")
	relayInstanceID := flag.String("instance-id", "", "persistent relay instance ID (enables reconnect without re-pairing)")
	workDir := flag.String("workdir", ".", "default working directory for worker sessions")
	model := flag.String("model", "", "default model for Grok workers (empty = Grok CLI default)")
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
	if explicit["jevons-model"] {
		cfg.OverseerModel = *overseerModel
	}

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

	// The overseer's MCP server is registered user-scoped in
	// ~/.grok/config.toml (see ensureGrokMCPServer, called before launch),
	// which attaches on both session/new and session/load — so we no
	// longer pass a session-scoped ACP param (mcp.claudia.json), which the
	// CLI honours only on session/new (🎯T58). Remove any stale copy so
	// claudia doesn't double-register the same server name. Use the
	// concrete bind address, never "localhost": the loopback-only default
	// (🎯T6) binds IPv4 127.0.0.1, and localhost may resolve to ::1.
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

	// Create components — Grok-only; sessions live under cfg.SessionsDir.
	scanner := discovery.NewScanner(cfg.SessionsDir)

	srv := server.New(cli.Version, cfg.StateDir)
	srv.SetOverseerName(cfg.OverseerName)

	// Durable conversation log (🎯T30.1): every chat line is fsynced here
	// and replayed to reconnecting clients — no conversation is ever lost.
	clog, err := chatlog.Open(filepath.Join(cfg.StateDir, "chatlog", cfg.OverseerName+".jsonl"))
	if err != nil {
		slog.Error("cannot open chat log", "err", err)
		os.Exit(1)
	}
	srv.SetChatLog(clog)

	// Durable decision/lifecycle journal (🎯T120): browser + server events
	// under state_dir/logs/events.jsonl — tool-readable without privilege.
	if elog, err := eventlog.Open(eventlog.DefaultPath(cfg.StateDir)); err != nil {
		slog.Error("cannot open event log", "err", err)
		// Non-fatal: continue with slog-only browser path.
	} else {
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
			tr := transcript.NewReader(cfg.SessionsDir)
			return tr.Read(sessionID)
		},
		Truncate: func(sessionID string, keepTurns int) error {
			tr := transcript.NewReader(cfg.SessionsDir)
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

	// 🎯T40: SIGINT/SIGTERM = normal stop (StopAll); SIGHUP = upgrade exit
	// (skip StopAll, write handles). JEVONS_UPGRADE_EXIT=1 also marks
	// SIGTERM/SIGINT as upgrade when launchd cannot send SIGHUP.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

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

	// Prior upgrade handoff: session_ids to resume (conversation path).
	// Process reattach is residual until claudia connect-mode exists.
	if snap, err := upgrade.LoadSnapshot(upgrade.SnapshotPath(cfg.StateDir)); err != nil {
		slog.Error("upgrade handles load failed (malformed handoff is hard error)", "err", err)
		os.Exit(1)
	} else if snap != nil {
		plan := upgrade.PlanReattach(snap)
		slog.Info("upgrade handoff present — reattach by session_id (conversation load); process reattach residual",
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
	fleetAdapter := fleet.NewClaudia(registry)
	btlrCfg := butler.Config{
		Store:        threadStore,
		Scanner:      scanner,
		Reader:       transcript.NewReader(cfg.SessionsDir),
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

	// Process-as-cache GC: periodically stop idle spawned threads'
	// processes (resumably) to free resources. The threads persist and
	// rehydrate on the next Direct.
	go reapIdleThreads(ctx, btlr)

	// Transcript memory is now provided by the standalone mnemo MCP server.
	// See https://github.com/marcelocantos/mnemo

	// Overseer: Grok Session ACP only.
	overseerModelChoice := cfg.OverseerModel
	if overseerModelChoice == "" {
		overseerModelChoice = cfg.Model
	}
	jevonDef, err := registry.EnsureAgent(cfg.OverseerName, jevDir, overseerModelChoice, true)
	if err != nil {
		slog.Error("jevon agent setup failed", "err", err)
		os.Exit(1)
	}
	jevonDef.Provider = cli.Provider
	// 🎯T114: overseer purpose on the unified participant record.
	if jevonDef.Purpose == "" {
		jevonDef.Purpose = claudia.PurposeOverseer
	}

	// The overseer's MCP tools come from a USER-SCOPED ~/.grok/config.toml
	// entry (🎯T58), which the Grok CLI attaches on BOTH session/new and
	// session/load — unlike the session-scoped ACP param, which it honours
	// only on session/new. That lets the overseer RESUME its real session
	// on restart (full model context) instead of rotating to a fresh
	// session and re-priming with a lossy recap. Register before launch so
	// the tools are present the moment the agent starts.
	if err := ensureGrokMCPServer(cfg, mcpHost); err != nil {
		slog.Warn("could not register jevons MCP server in ~/.grok/config.toml — overseer may start toolless", "err", err)
	}
	if err := registry.Register(*jevonDef); err != nil {
		slog.Error("jevon agent register failed", "err", err)
		os.Exit(1)
	}
	slog.Info("jevon agent", "provider", jevonDef.Provider, "session", jevonDef.SessionID, "resume", jevonDef.Materialized)

	srv.SetRegistry(registry)
	// 🎯T82: event-driven fleet panel refresh when agents.json mutates.
	srv.WatchAgentsFile(registryPath)

	listenAddr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)

	// Build the handler, optionally wrapping with mTLS middleware.
	var handler http.Handler = mux
	if *enableTLS {
		handler = server.ClientCertMiddleware(mux, "/health", "/api/provision")
	}
	httpSrv := &http.Server{Addr: listenAddr, Handler: handler}

	// Start HTTP server before agents so the MCP endpoint is reachable.
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("listen failed", "err", err)
		os.Exit(1)
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
	// StartAll resumes session_ids from agents.json (conversation durability).
	// True process reattach (same PID/stdio) needs claudia connect-mode (🎯T40 residual).
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
		mcpSrv.SetAgentEventHook(func(name string, ev claudia.Event) {
			if srv.ObserveAgentProgress(name, ev) {
				srv.NotifyAgentsChanged()
			}
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
		// The overseer did not launch — almost always a missing/unauth
		// Grok CLI (StartAll logged "auto-start failed"), so the chat
		// cannot respond. Fail LOUD and legible so a first-run stranger
		// knows exactly what to do instead of facing a silent chat
		// (🎯T54); the UI-facing half is the chat handler's overseer-down
		// path.
		reason := overseerUnavailableReason()
		slog.Error("OVERSEER NOT RUNNING — chat cannot respond until this is fixed",
			"overseer", cfg.OverseerName, "likely_cause", reason)
		srv.SetOverseerDownReason(reason)
	}

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

// overseerUnavailableReason returns a legible, actionable explanation for
// why the overseer could not launch (🎯T54). The overwhelmingly common
// first-run cause is a missing or unauthenticated Grok CLI, so it checks
// for the `grok` binary the same way claudia resolves it and tailors the
// message accordingly.
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

// ensureGrokMCPServer registers jevonsd's MCP endpoint user-scoped in
// ~/.grok/config.toml so the Grok CLI attaches it on BOTH session/new and
// session/load — the key to the overseer resuming its real session across
// restarts (🎯T58) instead of the old rotate-and-recap hack. Idempotent:
// removes any prior entry (ignoring the not-found error) then re-adds the
// current URL, so a changed port/bind is always reflected.
func ensureGrokMCPServer(cfg config.Config, host string) error {
	name, url := grokMCPServerSpec(cfg, host)
	grok := grokBin()
	_ = exec.Command(grok, "mcp", "remove", name).Run()
	out, err := exec.Command(grok, "mcp", "add", "--transport", "http", name, url).CombinedOutput()
	if err != nil {
		return fmt.Errorf("grok mcp add %s %s: %w (%s)", name, url, err, strings.TrimSpace(string(out)))
	}
	slog.Info("registered overseer MCP server (user-scoped, survives session/load)", "name", name, "url", url)
	return nil
}

// grokMCPServerSpec derives the user-scoped MCP registration for the
// overseer: the config.toml server name and the streamable-HTTP endpoint
// URL. The path must be exactly /mcp (where mcpserver mounts) and the host
// must be a concrete address — never "localhost", which can resolve to ::1
// while the loopback default binds IPv4 127.0.0.1 (🎯T6/🎯T58).
func grokMCPServerSpec(cfg config.Config, host string) (name, url string) {
	return cfg.MCPServerName, fmt.Sprintf("http://%s:%d/mcp", host, cfg.Port)
}

func overseerUnavailableReason() string {
	if _, err := exec.LookPath("grok"); err == nil {
		return "the Grok agent failed to start — check that the Grok CLI is signed in " +
			"(`grok login`, or set XAI_API_KEY) and see the jevonsd log for details"
	}
	for _, p := range grokCandidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return "the Grok agent failed to start — check that the Grok CLI at " + p +
				" is signed in (`grok login`, or set XAI_API_KEY)"
		}
	}
	return "the Grok CLI is not installed — install Grok Build and sign in " +
		"(`grok login`, or set XAI_API_KEY), then restart jevonsd. jevons runs its " +
		"overseer and workers as Grok agents and cannot operate without it"
}

// reapIdleGCInterval is how often the butler sweeps for idle spawned
// threads whose processes can be stopped resumably.
const reapIdleGCInterval = 2 * time.Minute

// reapIdleThreads runs the process-as-cache GC sweep until ctx is done.
func reapIdleThreads(ctx context.Context, btlr *butler.Butler) {
	ticker := time.NewTicker(reapIdleGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reaped := btlr.ReapIdle(); len(reaped) > 0 {
				slog.Info("reaped idle thread processes", "threads", reaped)
			}
		}
	}
}
