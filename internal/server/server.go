// Package server implements the HTTP/WebSocket server for daisd,
// handling remote client connections and routing messages through
// the Jevon coordinator. Multiple clients can connect simultaneously
// and all observe the same session.
package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/pigeon"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/auth"
	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/transcript"
	"github.com/marcelocantos/jevons/internal/workers"
)

// remoteWriter abstracts over WebSocket and tern relay connections.
type remoteWriter interface {
	WriteText(ctx context.Context, data []byte) error
	WriteBinary(ctx context.Context, data []byte) error
	Close() error
}

// wsWriter wraps a coder/websocket.Conn.
type wsWriter struct{ conn *websocket.Conn }

func (w wsWriter) WriteText(ctx context.Context, data []byte) error {
	return w.conn.Write(ctx, websocket.MessageText, data)
}
func (w wsWriter) WriteBinary(ctx context.Context, data []byte) error {
	return w.conn.Write(ctx, websocket.MessageBinary, data)
}
func (w wsWriter) Close() error { return w.conn.CloseNow() }

type remoteConn struct {
	writer remoteWriter
	ctx    context.Context
}

// Server is the daisd HTTP/WebSocket server.
type Server struct {
	version            string
	stateDir           string // jevons state root (config-driven, 🎯T44/T49)
	overseerName       string // registry name of the CEO agent (config-driven, 🎯T44)
	overseerDownReason string // legible cause when the overseer isn't running (🎯T54); guarded by mu
	ca                 *auth.CA

	mu        sync.RWMutex
	remoteSeq int
	remotes   map[int]remoteConn
	turnBuf   string // accumulates Jevon text for current turn
	waiting   bool   // true while awaiting a response from Jevon

	// chatConns is the number of live /ws/chat handlers (🎯T140 connect spans).
	// Guarded by mu; used only for concurrent=N on open/close logging.
	chatConns int

	lanSrv         *pigeon.LANServer // LAN server for direct connections
	creds          *CredentialStore
	openAIKey      string
	voiceBridge    *VoiceBridge
	lastScreenshot string
	screenshotCh   chan string
	proc           *claudia.Agent
	registry       *claudia.Registry
	chatListeners  []chan string
	chatLog        *chatlog.Log // durable conversation record (🎯T30.1)

	// activityHook fires on owner activity (a chat message), feeding the
	// budget dead-man's switch. costSource returns the latest cost
	// snapshot for GET /api/cost. Both default to no-ops.
	activityHook func()
	costSource   func() any

	// tokenLimiter rate-limits POST /api/realtime/token (T38 / Fable F4).
	tokenLimiter *tokenRateLimiter

	// peerSessionFactory builds pure sqlpipe Peer sessions for /ws/sqlpipe
	// (🎯T10 pure transport residual). Nil → endpoint fails closed.
	peerSessionFactory PeerSessionFactory

	// Async notifications (worker replies, budget alerts) bound for the
	// overseer. Its ACP session takes one prompt at a time, so a note that
	// arrives mid-turn is queued here and flushed on the next turn-complete
	// rather than dropped (🎯T62). Guarded by mu. notifySender is the
	// delivery seam (nil = live overseer process); overridable in tests.
	notifyQueue    []string
	notifyDraining bool
	notifySender   func(string) error

	// agentsWatchCancel stops the 🎯T82 registry file watcher (if any).
	agentsWatchCancel context.CancelFunc

	// frontierCwd is the primary workdir for bullseye ledger discovery (🎯T131).
	// Path resolution always goes through bullseye CLI — never hard-coded yaml.
	frontierCwd string
	// frontierWatchCancel / frontierWatchPath: fsnotify on resolved ledger (🎯T131).
	frontierWatchCancel context.CancelFunc
	frontierWatchPath   string

	// workers is the 🎯T8.2 observability tracker (SQLite + SSE hub).
	workers *workers.Tracker

	// agentProgress is live ACP-derived status for RHS fleet rows (🎯T118).
	agentProgress *AgentProgressHub

	// eventLog is the durable decision/lifecycle journal (🎯T120):
	// state_dir/logs/events.jsonl — browser + server events, tool-readable.
	eventLog *eventlog.Journal

	// transcriptReader reads Grok session chat_history for RHS inspect (🎯T124).
	transcriptReader *transcript.Reader

	// defaultProvider is the daemon-wide claudia backend for new asides
	// and other registry mint paths on this server (🎯T148).
	defaultProvider claudia.Provider
}

// SetActivityHook registers a callback fired on owner activity — the
// budget clamp-down's heartbeat.
func (s *Server) SetActivityHook(f func()) { s.activityHook = f }

// SetCostSource registers the provider of the live cost snapshot served
// at GET /api/cost.
func (s *Server) SetCostSource(f func() any) { s.costSource = f }

// Credentials returns the server-side pairing credential store.
func (s *Server) Credentials() *CredentialStore { return s.creds }

// SetOverseerName sets the registry name of the CEO agent (config-driven).
func (s *Server) SetOverseerName(name string) {
	if name != "" {
		s.overseerName = name
	}
}

// SetDefaultProvider sets the daemon-wide claudia backend for new agents
// registered via HTTP paths such as asides (🎯T148).
func (s *Server) SetDefaultProvider(p claudia.Provider) {
	if p != "" {
		s.defaultProvider = p
	}
}

// resolvedDefaultProvider returns the effective default for mint paths.
func (s *Server) resolvedDefaultProvider() claudia.Provider {
	if s.defaultProvider != "" {
		return s.defaultProvider
	}
	return cli.ResolveProvider("", "")
}

// SetOverseerDownReason records a legible, actionable explanation for why
// the overseer is unavailable (e.g. the Grok CLI is missing). The chat
// handler surfaces it to connecting clients so a first-run stranger sees
// the cause instead of a silent, unresponsive chat (🎯T54).
func (s *Server) SetOverseerDownReason(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overseerDownReason = reason
}

// SetChatLog attaches the durable jevons-owned conversation log
// (🎯T30.1): every chat line broadcast is appended to it, and /ws/chat
// clients replay history from it instead of the provider's store.
func (s *Server) SetChatLog(l *chatlog.Log) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chatLog = l
}

// New creates a Server with the given version string, rooting its
// credential store under stateDir (🎯T44 — no compiled-in paths).
func New(version, stateDir string) *Server {
	s := &Server{
		version:       version,
		stateDir:      stateDir,
		overseerName:  defaultOverseerName,
		remotes:       make(map[int]remoteConn),
		creds:         NewCredentialStore(filepath.Join(stateDir, "credential.json")),
		chatListeners: make([]chan string, 0),
		tokenLimiter:  newTokenRateLimiter(defaultTokenMintLimit, time.Minute),
		agentProgress: NewAgentProgressHub(),
	}

	if rec, err := s.creds.Load(); err != nil {
		slog.Warn("failed to load credential", "path", s.creds.Path(), "err", err)
	} else if rec != nil {
		slog.Info("loaded pairing credential", "peer", rec.PeerInstanceID)
	}

	return s
}

// HandleAgentEvent processes a JSONL event from the Jevon agent —
// accumulates assistant text into the current turn buffer, emits
// thinking/idle status to clients, and injects completed turns into
// the Grok voice bridge for TTS. Wired via Agent.SubscribeEvents in
// the daemon entry point.
func (s *Server) HandleAgentEvent(ev claudia.Event) {
	switch ev.Type {
	case "assistant":
		if ev.Text != "" {
			s.mu.Lock()
			s.turnBuf += ev.Text
			s.mu.Unlock()
			s.Broadcast(map[string]any{"type": "text", "content": ev.Text})
		}
		// Grok ACP ends a turn with an empty-text assistant event and
		// stop_reason=end_turn — there is no follow-up system event.
		// Treat terminal stops like the legacy system idle signal.
		if !ev.IsTerminalStop() {
			return
		}
		fallthrough
	case "system":
		s.mu.Lock()
		wasWaiting := s.waiting
		turnText := s.turnBuf
		s.turnBuf = ""
		s.waiting = false
		s.mu.Unlock()
		// The overseer's ACP session is now idle — flush any async notes
		// (worker replies, budget alerts) that arrived while it was busy and
		// got "prompt already in flight". Runs on every terminal stop, even
		// the completion of a note-injected turn, so a backlog drains one
		// note-batch per turn (🎯T62). Deferred so it also fires on the
		// !wasWaiting early return below.
		defer s.drainOverseerNotes()
		if !wasWaiting {
			return
		}
		s.Broadcast(map[string]any{"type": "status", "state": "idle"})
		// Pre-🎯T18: voiceBridge.InjectResponse(turnText) used to be
		// called here so Grok would speak Claude's turn aloud. Removed
		// because Grok is now the overseer: it dispatches via tools and
		// receives completions through SendSystemNote. Re-invoking
		// InjectResponse here would double-handle every delegated turn.
		_ = turnText
	}
}

// BroadcastBinary sends a binary WebSocket message to all connected clients.
func (s *Server) BroadcastBinary(data []byte) {
	if len(data) == 0 {
		return
	}

	s.mu.RLock()
	remotes := make([]remoteConn, 0, len(s.remotes))
	for _, rc := range s.remotes {
		remotes = append(remotes, rc)
	}
	s.mu.RUnlock()

	for _, rc := range remotes {
		writeCtx, cancel := context.WithTimeout(rc.ctx, 5*time.Second)
		if err := rc.writer.WriteBinary(writeCtx, data); err != nil {
			slog.Debug("binary broadcast write failed", "err", err)
		}
		cancel()
	}
}

// RegisterRoutes adds HTTP and WebSocket routes to the mux.
// Additional routes (e.g. MCP server) should be registered separately.
// Static file serving is handled by DevServer.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /api/provision", s.handleProvision)
	mux.HandleFunc("/ws/chat", s.handleChat)
	mux.HandleFunc("/ws/remote", s.handleRemote)
	mux.HandleFunc("/ws/sqlpipe", s.handleSqlpipe) // 🎯T10 pure transport residual
	mux.HandleFunc("GET /api/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/agents/{name}/transcript", s.handleAgentTranscript)
	mux.HandleFunc("POST /api/asides", s.handleCreateAside)       // 🎯T136: register purpose=aside in fleet
	mux.HandleFunc("DELETE /api/asides/{id}", s.handleDeleteAside) // 🎯T152: dismiss fleet aside on target filed
	mux.HandleFunc("GET /api/frontier", s.handleFrontier) // 🎯T131: live bullseye frontier table
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/cost", s.handleCost)
	mux.HandleFunc("POST /api/log", s.handleBrowserLog)
	mux.HandleFunc("GET /api/logs", s.handleLogsTail)
	mux.HandleFunc("/ws/agent-terminal", s.handleAgentTerminal)
	mux.HandleFunc("POST /api/realtime/token", s.handleRealtimeToken)
	mux.HandleFunc("/ws/voice", s.handleVoice)
	s.registerWorkerRoutes(mux)
	s.registerImageRoutes(mux)
	s.registerSelfTestRoutes(mux)
}

func (s *Server) handleVoice(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	vb := s.voiceBridge
	s.mu.RUnlock()
	if vb == nil {
		// Accept the WebSocket so the client gets a clear JSON error
		// (failed upgrades don't surface error text to JavaScript).
		conn, err := websocket.Accept(w, r, wsAcceptOptions())
		if err != nil {
			return
		}
		data, _ := json.Marshal(map[string]any{
			"type":  "error",
			"error": "Voice not configured. Run: bin/jevonsd --set-xai-key",
		})
		conn.Write(r.Context(), websocket.MessageText, data)
		conn.Close(websocket.StatusNormalClosure, "no API key")
		return
	}
	vb.HandleVoiceWS(w, r)
}

// handleRealtimeToken proxies an ephemeral token request to the OpenAI
// Realtime API. The API key stays on the server; the client gets a
// short-lived token for direct WebSocket connection to OpenAI.
func (s *Server) handleRealtimeToken(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	if s.tokenLimiter != nil && !s.tokenLimiter.allow(time.Now()) {
		http.Error(w, `{"error":"token mint rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}
	apiKey := s.openAIKey
	if apiKey == "" {
		http.Error(w, `{"error":"OPENAI_API_KEY not configured (set env var or store in macOS Keychain: security add-generic-password -a jevon -s openai-api-key -w YOUR_KEY)"}`, http.StatusServiceUnavailable)
		return
	}

	body := `{"model":"gpt-4o-transcribe","voice":"alloy"}`
	req, err := http.NewRequestWithContext(r.Context(), "POST",
		"https://api.openai.com/v1/realtime/sessions", strings.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"request creation failed"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("openai realtime session failed", "err", err)
		http.Error(w, `{"error":"openai request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// SetOpenAIKey sets the OpenAI API key for Realtime API token proxying.
func (s *Server) SetOpenAIKey(key string) { s.openAIKey = key }

// SetVoiceBridge attaches a Grok voice bridge for the /ws/voice endpoint.
func (s *Server) SetVoiceBridge(vb *VoiceBridge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceBridge = vb
}

// VoiceBridgeRef returns the voice bridge, if configured.
func (s *Server) VoiceBridgeRef() *VoiceBridge { return s.voiceBridge }

// SetCA attaches a CA for mTLS device provisioning.
func (s *Server) SetCA(ca *auth.CA) { s.ca = ca }

// provisionRequest is the JSON body for POST /api/provision.
type provisionRequest struct {
	DeviceID  string `json:"device_id"`
	PublicKey string `json:"public_key"` // base64-encoded Ed25519 public key
}

// provisionResponse is the JSON body returned by POST /api/provision.
type provisionResponse struct {
	Certificate   string `json:"certificate"`    // PEM-encoded client cert
	CACertificate string `json:"ca_certificate"` // PEM-encoded CA cert
}

// handleProvision issues a client certificate for a new device.
// This endpoint is intentionally accessible without a client certificate —
// it is the bootstrap step for provisioning new devices — but cross-site
// browser POSTs are still rejected (T38 / Fable F4).
func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	if s.ca == nil {
		http.Error(w, `{"error":"CA not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req provisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" {
		http.Error(w, `{"error":"device_id is required"}`, http.StatusBadRequest)
		return
	}
	if req.PublicKey == "" {
		http.Error(w, `{"error":"public_key is required"}`, http.StatusBadRequest)
		return
	}

	keyBytes, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		http.Error(w, `{"error":"public_key must be base64-encoded"}`, http.StatusBadRequest)
		return
	}
	if len(keyBytes) != ed25519.PublicKeySize {
		http.Error(w, `{"error":"public_key must be a 32-byte Ed25519 public key"}`, http.StatusBadRequest)
		return
	}
	pubKey := ed25519.PublicKey(keyBytes)

	certPEM, err := s.ca.IssueCert(pubKey, req.DeviceID)
	if err != nil {
		slog.Error("provision: IssueCert failed", "device_id", req.DeviceID, "err", err)
		http.Error(w, `{"error":"certificate issuance failed"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("device provisioned", "device_id", req.DeviceID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(provisionResponse{
		Certificate:   string(certPEM),
		CACertificate: string(s.ca.CertPEM()),
	})
}

// ClientCertMiddleware wraps an http.Handler and enforces that the client
// presented a valid certificate on all routes except those in exemptPaths.
// Use this when the TLS config has ClientAuth: VerifyClientCertIfGiven so
// that provisioning endpoints remain reachable before a cert is issued.
func ClientCertMiddleware(next http.Handler, exemptPaths ...string) http.Handler {
	exempt := make(map[string]bool, len(exemptPaths))
	for _, p := range exemptPaths {
		exempt[p] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exempt[r.URL.Path] {
			if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
				http.Error(w, `{"error":"client certificate required"}`, http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": s.version,
	})
}

func (s *Server) handleRemote(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, wsAcceptOptions())
	if err != nil {
		slog.Error("remote accept failed", "err", err)
		return
	}
	defer conn.CloseNow()

	conn.SetReadLimit(1 << 20) // 1 MB

	ctx := r.Context()

	// Register this connection.
	s.mu.Lock()
	s.remoteSeq++
	remoteID := s.remoteSeq
	s.remotes[remoteID] = remoteConn{writer: wsWriter{conn: conn}, ctx: ctx}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.remotes, remoteID)
		s.mu.Unlock()
	}()

	slog.Info("remote connected", "clients", len(s.remotes))

	// Send init. History is no longer carried in-memory — Claude's
	// JSONL session file at proc.JSONLPath() is the canonical record.
	s.writeJSON(conn, ctx, map[string]any{
		"type":    "init",
		"version": s.version,
	})

	// Read loop: process messages from remote.
	for {
		mt, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Info("remote disconnected", "clients", len(s.remotes)-1)
			}
			return
		}

		// Skip binary messages.
		if mt == websocket.MessageBinary {
			continue
		}

		if mt != websocket.MessageText {
			continue
		}

		var msg struct {
			Type   string `json:"type"`
			Text   string `json:"text,omitempty"`
			Action string `json:"action,omitempty"`
			Value  string `json:"value,omitempty"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "control":
			s.handleControl(conn, ctx, msg.Action, msg.Value)

		case "message":
			s.HandleUserMessage(msg.Text)

		case "action":
			s.HandleAction(msg.Action, msg.Value)
		}
	}
}

// Broadcast sends a JSON message to all connected remote clients.
func (s *Server) Broadcast(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal failed", "err", err)
		return
	}

	s.mu.RLock()
	remotes := make([]remoteConn, 0, len(s.remotes))
	for _, rc := range s.remotes {
		remotes = append(remotes, rc)
	}
	s.mu.RUnlock()

	for _, rc := range remotes {
		writeCtx, cancel := context.WithTimeout(rc.ctx, 5*time.Second)
		if err := rc.writer.WriteText(writeCtx, data); err != nil {
			slog.Debug("broadcast write failed", "err", err)
		}
		cancel()
	}
}

// HandleUserMessage processes a text message from a remote client.
func (s *Server) HandleUserMessage(text string) {
	if text == "" {
		return
	}

	s.Broadcast(map[string]any{
		"type":      "user_message",
		"text":      text,
		"timestamp": time.Now(),
	})

	s.mu.RLock()
	proc := s.proc
	s.mu.RUnlock()
	if proc == nil {
		slog.Warn("jevons: message received before claude started")
		return
	}
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	s.Broadcast(map[string]any{"type": "status", "state": "thinking"})
	if err := proc.Send(text); err != nil {
		slog.Error("jevons: send failed", "err", err)
	}
}

// HandleAction processes a UI action from a remote client or a timer callback.
func (s *Server) HandleAction(action, value string) {
	if action == "" {
		return
	}
	slog.Debug("action received", "action", action, "value", value)

	// send_message always goes through HandleUserMessage for persistence
	// and broadcast, regardless of Lua handling.
	switch {
	case action == "send_message":
		s.HandleUserMessage(value)

	case action == "dismiss_sheet":
		// Client handles dismiss locally.

	case action == "disconnect":
		slog.Info("disconnect requested via action")

	default:
		slog.Warn("unknown action", "action", action)
	}
}

// handleControl processes control-channel messages that bypass the Lua layer.
// These are used for safe mode operations (rollback, version query, health).
func (s *Server) handleControl(conn *websocket.Conn, ctx context.Context, action, value string) {
	respond := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		conn.Write(writeCtx, websocket.MessageText, data)
	}

	switch action {
	case "health":
		respond(map[string]any{
			"type":   "control",
			"action": "health",
			"status": "ok",
		})

	case "list_snapshots":
		respond(map[string]any{
			"type":   "control",
			"action": "list_snapshots",
			"error":  "sync not available",
		})

	case "screenshot":
		// Forward screenshot request to all connected clients.
		s.Broadcast(map[string]any{
			"type":   "control",
			"action": "screenshot",
		})
		// Don't respond yet — the client will send screenshot_result.

	case "screenshot_result":
		// Client sent back a screenshot as base64 PNG in the value field.
		if value == "" {
			slog.Warn("screenshot_result: no data")
			return
		}
		imgData, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			slog.Error("screenshot_result: decode failed", "err", err)
			return
		}
		path := filepath.Join(os.TempDir(), "jevons-screenshot.png")
		if err := os.WriteFile(path, imgData, 0644); err != nil {
			slog.Error("screenshot_result: write failed", "err", err)
			return
		}
		slog.Info("screenshot saved", "path", path)
		s.mu.Lock()
		s.lastScreenshot = path
		if s.screenshotCh != nil {
			select {
			case s.screenshotCh <- path:
			default:
			}
		}
		s.mu.Unlock()

	case "rollback":
		respond(map[string]any{
			"type":   "control",
			"action": "rollback",
			"error":  "sync not available",
		})

	default:
		slog.Warn("unknown control action", "action", action)
	}
}

// RequestScreenshot sends a screenshot request to connected clients and waits
// for the result. Returns the file path of the saved PNG.
func (s *Server) RequestScreenshot(timeout time.Duration) (string, error) {
	ch := make(chan string, 1)
	s.mu.Lock()
	s.screenshotCh = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.screenshotCh = nil
		s.mu.Unlock()
	}()

	s.Broadcast(map[string]any{
		"type":   "control",
		"action": "screenshot",
	})

	select {
	case path := <-ch:
		return path, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("screenshot timeout")
	}
}

// writeJSON sends a JSON message to a single connection.
func (s *Server) writeJSON(conn *websocket.Conn, ctx context.Context, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal failed", "err", err)
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		slog.Debug("write failed", "err", err)
	}
}
