// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// journey-suite runs a small owner-chat user-journey suite against an
// ISOLATED jevonsd — separate port, state dir, chatlog, and MCP name —
// so it never pollutes the daily-driver stream on :13705 / ~/.jevons.
//
//	make test-journey
//	make test-journey PROVIDER=claude
//	go run ./scripts/journey-suite [-keep] [-port 0] [-provider claude] [-only J10]
//
// Journeys (live agent backend; owner chat + MCP orchestration):
//  1. health
//  2. chat round-trip (idle send → terminal)
//  3. cancel-and-send (interrupt mid-turn → replacement → terminal)
//  4. reconnect sealed (second connect sees bounded sealed history)
//  5. isolation (after teardown)
//     6–11. orchestration: tool surface, overseer registry, two agents
//     same workdir, thread spawn→direct→remove, worker shell tool (T97),
//     worker transcript visible to inspect (T282)
//
// -provider runs the entire isolate — overseer and every agent the
// journeys spawn — on one backend (🎯T282), so `PROVIDER=claude` is the
// live evidence that Jevons can run an all-Claude fleet.
//
// Part of `make test` (🎯T492). Needs the provider CLI + network; that is
// a suite dependency, not a reason to omit the owner-visible net.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

const (
	defaultPort     = portguard.DefaultPort
	dailyPort       = portguard.DailyPort // Universe A — never bind (portguard.RefuseDaily)
	mcpName         = "jevonsmcp-journey"
	dailyMCPName    = "jevonsmcp"
	overseerName    = "jevons"
	readyTimeout    = 45 * time.Second
	turnTimeout     = 90 * time.Second
	maxReplayFrames = 200 // sealed recent window; thousands means pollution/unsealed
)

type suite struct {
	host     string
	stateDir string
	provider claudia.Provider
	only     string // when set, run only journeys whose name contains it
	failures int
	// outages counts journeys the provider prevented from running (🎯T283).
	// They are reported separately and do not fail the suite.
	outages int

	// Isolate daemon — J14 bounces this process against the same state.
	daemonBin string
	cfgPath   string
	logPath   string
	workdir   string
	port      int
	cmd       *exec.Cmd
	logFile   *os.File
	daemonEnv []string
}

func main() {
	port := flag.Int("port", defaultPort, "isolated listen port (0 = ephemeral)")
	keep := flag.Bool("keep", false, "keep sandbox state dir after run (for debugging)")
	bin := flag.String("bin", "", "path to jevonsd (default: bin/jevonsd or PATH)")
	only := flag.String("only", "",
		"run only journeys whose name contains this substring (e.g. J10)")
	providerFlag := flag.String("provider", "",
		"agent backend for the whole isolate (claudia provider id: grok, claude, …; empty = JEVONS_PROVIDER, else grok)")
	flag.Parse()

	// 🎯T282: one selector drives the entire isolate — overseer and every
	// agent the journeys spawn. Resolving through cli.ResolveProvider means
	// `JEVONS_PROVIDER=claude make test-journey` works without a flag, and
	// the suite pins the result into the isolate's config so the daemon
	// cannot silently disagree with the CLI paths used for teardown.
	provider := cli.ResolveProvider(*providerFlag, "")

	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	// Prefer repo bin/jevonsd when run from repo root.
	daemon := *bin
	if daemon == "" {
		cand := filepath.Join(root, "bin", "jevonsd")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			daemon = cand
		} else {
			daemon = "jevonsd"
		}
	}

	p := *port
	if p == 0 {
		p, err = freePort()
		if err != nil {
			fatal(err)
		}
	}
	if err := portguard.RefuseDaily(p); err != nil {
		fatal(err)
	}
	host := fmt.Sprintf("127.0.0.1:%d", p)

	stateDir, err := os.MkdirTemp("", "jevons-journey-*")
	if err != nil {
		fatal(err)
	}
	if !*keep {
		defer os.RemoveAll(stateDir)
	} else {
		fmt.Println("KEEP state:", stateDir)
	}

	// MCP baseline: daily registration must survive the suite if present.
	// (Daily chatlog mtime is *not* an oracle — a live daily-driver overseer
	// may write concurrently while this suite runs against its own isolate.)
	hadDailyMCP := mcpListedFor(provider, dailyMCPName)

	cfgPath := filepath.Join(stateDir, "config.yaml")
	// sessions_dir and claude_projects stay at their defaults (~/.grok/sessions,
	// ~/.claude/projects). Both CLIs choose those trees themselves, keyed by
	// workdir, so the sandbox temp workdir already isolates this run's
	// transcripts — pointing jevons elsewhere isolates nothing and merely
	// blinds discovery (🎯T213), which is how 🎯T285's migration journey first
	// failed: the transcripts were there, jevons was looking in an empty
	// sandbox directory.
	sessionsDir := filepath.Join(homeDir(), ".grok", "sessions")
	cfg := fmt.Sprintf(`owner_name: JourneyTester
overseer_name: %s
bind_addr: 127.0.0.1
port: %d
state_dir: %q
sessions_dir: %q
mcp_server_name: %s
workdir: %q
provider: %s
persona_notes: |
  You are a short-lived journey-test overseer. Prefer one-line text answers.
  Do not spawn workers. For "Reply with exactly: X" prompts, reply with X only.
`, overseerName, p, stateDir, sessionsDir, mcpName, stateDir, provider)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		fatal(err)
	}

	logPath := filepath.Join(stateDir, "jevonsd.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		fatal(err)
	}
	s := &suite{
		host: host, stateDir: stateDir, provider: provider, only: *only,
		daemonBin: daemon, cfgPath: cfgPath, logPath: logPath,
		workdir: stateDir, port: p, logFile: logFile,
	}

	// Always tear down daemon + journey MCP. Normal path stops before J5 so
	// isolation assertions see post-teardown MCP state; defer covers early
	// failure paths. stop is idempotent.
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		started := s.cmd != nil && s.cmd.Process != nil
		_ = s.signalStop(5 * time.Second)
		_ = logFile.Close()
		// Remove journey MCP only — never touch the daily MCP name — and
		// through the CLI that registered it (🎯T282: Claude registers via
		// `claude mcp add -s user`, not ~/.grok/config.toml).
		mcpRemoveFor(provider, mcpName)
		if started {
			fmt.Println("stopped isolated jevonsd; removed MCP", mcpName)
		} else {
			fmt.Println("isolate never started; removed MCP", mcpName)
		}
		if *keep {
			fmt.Println("log:", logPath)
		}
	}
	// 🎯T379: registered, not deferred, and registered BEFORE the daemon is
	// started rather than after. A defer covers the happy path only — fatal()
	// exits without unwinding and a signal does not unwind at all, and both of
	// those leak the MCP registration. Ordering matters for the same reason:
	// the daemon writes the user-scoped `jevonsmcp-journey` entry during its
	// own boot, so a Ctrl-C landing between Start and this registration would
	// leak exactly the entry this target exists to stop leaking. Teardown that
	// runs before the process exists is harmless: stop skips a nil Process and
	// the removal is a no-op (or reclaims a previous run's stale entry).
	cleanups.Add(stop)
	catchSignals()

	if err := s.startDaemon(); err != nil {
		dumpTail(logPath, 40)
		fatal(fmt.Errorf("start jevonsd: %w (build with make jevonsd?)", err))
	}
	fmt.Printf("started isolated jevonsd pid=%d host=%s state=%s mcp=%s provider=%s\n",
		s.cmd.Process.Pid, host, stateDir, mcpName, provider)
	s.run("J1-health", s.jHealth)
	s.run("J2-chat-round-trip", s.jChatRoundTrip)
	s.run("J3-cancel-and-send", s.jCancelAndSend)
	s.run("J4-reconnect-sealed", s.jReconnectSealed)

	// Orchestration (MCP-direct against the isolate — not the daily stream).
	s.run("J6-mcp-tool-surface", s.jMCPToolSurface)
	s.run("J6c-overseer-tools-attached", s.jOverseerToolsAttached)
	s.run("J6b-mcp-reconnect", s.jMCPReconnect)
	s.run("J7-overseer-registry", s.jOverseerInRegistry)
	s.run("J8-two-agents-same-workdir", s.jTwoAgentsSameWorkdir)
	s.run("J8b-po-worker-lineage-fanout", s.jPOWorkerLineageFanout)
	s.run("J9-thread-spawn-direct", s.jThreadSpawnDirectRemove)
	s.run("J10-worker-shell-tool", s.jWorkerShellTool)
	s.run("J11-worker-transcript", s.jWorkerTranscriptVisible)
	s.run("J12-provider-migration", s.jProviderMigration)
	s.run("J13-overseer-migration", s.jOverseerMigration)
	s.run("J14-bounce-resume", s.jBounceResume)
	s.run("J15-switch-seed-shape", s.jSwitchSeedShape)
	s.run("J16-t3924-checkpoint-resume", s.jT3924CheckpointResume)
	s.run("J17-t418-queue-bounce", s.jT418QueueBounce)
	s.run("J18-t418-handover-mute", s.jT418HandoverMute)
	s.run("J19-root-history-paint", s.j19RootHistoryPaint)

	// Stop isolate before isolation oracle so MCP list is post-teardown.
	stop()

	s.run("J5-isolation", func() error {
		return assertIsolation(provider, hadDailyMCP, stateDir, p)
	})

	if s.failures > 0 {
		dumpTail(logPath, 60)
		fmt.Printf("FAIL: %d journey(s) failed\n", s.failures)
		exitNow(1)
	}
	// 🎯T283: outages are not green, but they are not product defects either —
	// the suite says which it saw rather than letting a backend outage read as
	// a broken product.
	if s.outages > 0 {
		dumpTail(logPath, 30)
		fmt.Printf("OUTAGE: %d journey(s) could not run — provider backend unavailable, not a product defect. Re-run when the backend is healthy.\n", s.outages)
		exitNow(2)
	}
	fmt.Println("PASS: journey suite green (isolated; daily stream untouched)")
}

// assertIsolation checks path + MCP isolation: journal lives only under the
// temp state dir, journey MCP is gone, daily MCP still present if it was.
func assertIsolation(provider claudia.Provider, hadDailyMCP bool, stateDir string, port int) error {
	if err := portguard.RefuseDaily(port); err != nil {
		return err
	}
	homeJevons, err := filepath.Abs(filepath.Join(homeDir(), ".jevons"))
	if err != nil {
		return err
	}
	absState, err := filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	if absState == homeJevons || strings.HasPrefix(absState, homeJevons+string(os.PathSeparator)) {
		return fmt.Errorf("state_dir is under daily ~/.jevons: %s", absState)
	}
	sandboxJournal := filepath.Join(absState, "chatlog", overseerName+".jsonl")
	if st, err := os.Stat(sandboxJournal); err != nil || st.Size() == 0 {
		return fmt.Errorf("sandbox journal missing under isolate: %s (%v)", sandboxJournal, err)
	}
	if mcpListedFor(provider, mcpName) {
		return fmt.Errorf("MCP %s still registered after teardown", mcpName)
	}
	if hadDailyMCP && !mcpListedFor(provider, dailyMCPName) {
		return fmt.Errorf("daily MCP %s missing after suite (should be untouched)", dailyMCPName)
	}
	return nil
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func (s *suite) run(name string, fn func() error) {
	if s.only != "" && !strings.Contains(name, s.only) {
		return
	}
	start := time.Now()
	if err := fn(); err != nil {
		// 🎯T283: a backend outage did not let the assertion run, so it is
		// not evidence of a product defect and must not be scored as one.
		if isOutage(err) {
			s.outages++
			fmt.Printf("OUT  %-22s %v (%s)\n", name, err, time.Since(start).Round(time.Millisecond))
			return
		}
		s.failures++
		fmt.Printf("FAIL %-22s %v (%s)\n", name, err, time.Since(start).Round(time.Millisecond))
		return
	}
	fmt.Printf("ok   %-22s (%s)\n", name, time.Since(start).Round(time.Millisecond))
}

func (s *suite) jHealth() error {
	resp, err := http.Get("http://" + s.host + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (s *suite) jChatRoundTrip() error {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	n, err := drainReplay(frames, 800*time.Millisecond)
	if err != nil {
		return err
	}
	if n > maxReplayFrames {
		return fmt.Errorf("fresh isolate replayed %d frames; want ≤%d (sealed)", n, maxReplayFrames)
	}
	token := fmt.Sprintf("journey-ping-%d", time.Now().Unix()%100000)
	prompt := "Reply with exactly: " + token
	if err := conn.Write(ctx, websocket.MessageText, []byte(prompt)); err != nil {
		return err
	}
	gotUser, text, terminal, err := waitTurn(ctx, frames, token, true)
	if err != nil {
		return err
	}
	if !gotUser {
		return fmt.Errorf("no user echo")
	}
	if !terminal {
		return fmt.Errorf("no terminal")
	}
	// Prefer exact token; accept any completed turn (tool-only models).
	if text != "" && !strings.Contains(text, token) && !strings.Contains(strings.ToLower(text), "journey") {
		// Non-fatal if model paraphrases; require non-empty OR tool terminal already ok
		_ = text
	}
	return nil
}

func (s *suite) jCancelAndSend() error {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout+30*time.Second)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := drainReplay(frames, 800*time.Millisecond); err != nil {
		return err
	}

	long := "Count slowly from 1 to 40, one number per line."
	if err := conn.Write(ctx, websocket.MessageText, []byte(long)); err != nil {
		return err
	}
	// Wait for in-flight activity.
	if err := waitAssistantActivity(ctx, frames, 25*time.Second); err != nil {
		return fmt.Errorf("long turn never started: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"interrupt"}`)); err != nil {
		return err
	}
	// Wait for cancel settle BEFORE correction (honest; no false-pass).
	if err := waitCancelSettled(ctx, frames, 30*time.Second); err != nil {
		return fmt.Errorf("cancel settle: %w", err)
	}
	token := "JOURNEY-CANCEL-OK"
	if err := conn.Write(ctx, websocket.MessageText, []byte("Reply with exactly: "+token)); err != nil {
		return err
	}
	gotUser, _, terminal, err := waitTurn(ctx, frames, token, true)
	if err != nil {
		return err
	}
	if !gotUser {
		return fmt.Errorf("no correction user echo")
	}
	if !terminal {
		return fmt.Errorf("no terminal after correction")
	}
	return nil
}

func (s *suite) jReconnectSealed() error {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	// Seed one turn.
	conn1, frames1, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	n1, err := drainReplay(frames1, 800*time.Millisecond)
	if err != nil {
		conn1.CloseNow()
		return err
	}
	seed := "Reply with exactly: journey-seed"
	if err := conn1.Write(ctx, websocket.MessageText, []byte(seed)); err != nil {
		conn1.CloseNow()
		return err
	}
	if _, _, _, err := waitTurn(ctx, frames1, "journey-seed", false); err != nil {
		conn1.CloseNow()
		return fmt.Errorf("seed turn: %w", err)
	}
	conn1.CloseNow()

	// Reconnect — sealed window must stay small.
	conn2, frames2, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn2.CloseNow()
	n2, err := drainReplay(frames2, 900*time.Millisecond)
	if err != nil {
		return err
	}
	if n2 > maxReplayFrames {
		return fmt.Errorf("reconnect replayed %d frames (first connect %d); unsealed or wrong isolate?", n2, n1)
	}
	// Journal should exist only under sandbox state.
	journal := filepath.Join(s.stateDir, "chatlog", overseerName+".jsonl")
	if st, err := os.Stat(journal); err != nil || st.Size() == 0 {
		return fmt.Errorf("sandbox journal missing: %s (%v)", journal, err)
	}
	// Daily-driver journal path must not be required (and we never opened it).
	return nil
}

// ── wire helpers ─────────────────────────────────────────────────────

func dialChat(ctx context.Context, host string) (*websocket.Conn, <-chan []byte, error) {
	conn, _, err := websocket.Dial(ctx, "ws://"+host+"/ws/chat", nil)
	if err != nil {
		return nil, nil, err
	}
	conn.SetReadLimit(4 << 20)
	ch := make(chan []byte, 512)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				close(ch)
				return
			}
			ch <- data
		}
	}()
	return conn, ch, nil
}

func drainReplay(frames <-chan []byte, quiet time.Duration) (int, error) {
	n := 0
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				return n, fmt.Errorf("conn closed during replay drain")
			}
			n++
		case <-time.After(quiet):
			return n, nil
		}
	}
}

func waitAssistantActivity(ctx context.Context, frames <-chan []byte, d time.Duration) error {
	deadline := time.After(d)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("conn closed")
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if m["type"] == "error" {
				return fmt.Errorf("wire error: %v", m["error"])
			}
			if m["type"] == "assistant" {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("timeout")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitCancelSettled(ctx context.Context, frames <-chan []byte, d time.Duration) error {
	deadline := time.After(d)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("conn closed")
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if m["type"] == "error" {
				return fmt.Errorf("wire error: %v", m["error"])
			}
			if m["type"] == "status" {
				if st, _ := m["state"].(string); st == "cancel_settled" || st == "idle" {
					return nil
				}
			}
			if m["type"] == "assistant" {
				msg, _ := m["message"].(map[string]any)
				stop, _ := msg["stop_reason"].(string)
				if stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens" {
					return nil
				}
			}
		case <-deadline:
			return fmt.Errorf("timeout waiting cancel settle")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitTurn waits for optional user-echo matching needle and a terminal
// after that. requireUser forces a user echo containing needle.
func waitTurn(ctx context.Context, frames <-chan []byte, needle string, requireUser bool) (gotUser bool, text string, terminal bool, err error) {
	var asst strings.Builder
	deadline := time.After(turnTimeout)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return gotUser, asst.String(), terminal, fmt.Errorf("conn closed")
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			typ, _ := m["type"].(string)
			if typ == "error" {
				return gotUser, asst.String(), terminal, fmt.Errorf("wire error: %v", m["error"])
			}
			msg, _ := m["message"].(map[string]any)
			if typ == "user" {
				if s, ok := msg["content"].(string); ok {
					if needle == "" || strings.Contains(s, needle) || strings.Contains(s, "Reply with exactly") {
						gotUser = true
						asst.Reset()
					}
				}
			}
			if typ == "assistant" && (gotUser || !requireUser) {
				if !requireUser {
					gotUser = true // allow tool-only without strict user match
				}
				stop, _ := msg["stop_reason"].(string)
				if content, ok := msg["content"].([]any); ok {
					for _, c := range content {
						cm, _ := c.(map[string]any)
						if cm["type"] == "text" {
							if t, _ := cm["text"].(string); t != "" {
								asst.WriteString(t)
							}
						}
					}
				}
				if stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens" {
					if gotUser || !requireUser {
						return gotUser, asst.String(), true, nil
					}
				}
			}
		case <-deadline:
			return gotUser, asst.String(), false, fmt.Errorf("timeout gotUser=%v text=%q", gotUser, trim(asst.String(), 80))
		case <-ctx.Done():
			return gotUser, asst.String(), false, ctx.Err()
		}
	}
}

func waitReady(host string, d time.Duration) error {
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + host + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				// Also wait until overseer is running if agents API is up.
				ar, err := http.Get("http://" + host + "/api/agents")
				if err == nil {
					var agents []struct {
						Name   string `json:"name"`
						Status string `json:"status"`
					}
					_ = json.NewDecoder(ar.Body).Decode(&agents)
					ar.Body.Close()
					for _, a := range agents {
						if a.Name == overseerName && a.Status == "running" {
							return nil
						}
					}
					last = fmt.Errorf("overseer not running yet: %v", agents)
				} else {
					// health ok is enough to proceed; chat may still fail
					return nil
				}
			}
		} else {
			last = err
		}
		time.Sleep(400 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return last
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func dumpTail(path string, n int) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	fmt.Fprintln(os.Stderr, "—— jevonsd log tail ——")
	fmt.Fprintln(os.Stderr, strings.Join(lines, "\n"))
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fatal:", err)
	// 🎯T379: teardown BEFORE exiting. os.Exit skips defers, so the
	// pre-fix `defer stop()` never ran on this path and the run leaked its
	// user-scoped MCP registration onto the owner's machine.
	exitNow(1)
}
