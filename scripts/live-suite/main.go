// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// live-suite drives a RUNNING jevonsd through the owner flows and
// asserts on ground truth (MCP responses, the chat journal, wire
// frames) — never on model prose alone (🎯T51). Two tiers:
//
//   - MCP-direct scenarios are deterministic (no LLM in the loop).
//   - Overseer scenarios exercise the prompt↔tool contract through
//     /ws/chat; each costs a few short model turns.
//
// Usage:
//
//	go run ./scripts/live-suite [-host 127.0.0.1:13705] [-ns jevonsmcp]
//	    [-state-dir ~/.jevons] [-overseer jevons] [-adopt-session <uuid>]
//	    [-skip-overseer]
//
// Exit 0 iff every executed scenario passes. Scenarios that mutate
// state (adopt) clean up after themselves (remove).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type suite struct {
	host     string
	ns       string
	stateDir string
	overseer string
	failures int
}

func main() {
	home, _ := os.UserHomeDir()
	host := flag.String("host", "127.0.0.1:13705", "jevonsd host:port")
	ns := flag.String("ns", "jevonsmcp", "MCP namespace the overseer uses (config mcp_server_name)")
	stateDir := flag.String("state-dir", filepath.Join(home, ".jevons"), "jevons state dir (chatlog ground truth)")
	overseer := flag.String("overseer", "jevons", "overseer name (chatlog filename)")
	adoptSession := flag.String("adopt-session", "", "session UUID for the adopt scenario (skipped when empty)")
	skipOverseer := flag.Bool("skip-overseer", false, "run only deterministic MCP-direct scenarios")
	spawnDir := flag.String("spawn-dir", "", "workdir for the spawn-direct scenario (skipped when empty; spawns a real worker)")
	restartCmd := flag.String("restart-cmd", "", "shell command that restarts the daemon (enables the restart-durability scenario)")
	enableRewind := flag.Bool("enable-rewind", false, "run the rewind scenario (trims the last overseer turn)")
	flag.Parse()

	s := &suite{host: *host, ns: *ns, stateDir: *stateDir, overseer: *overseer}

	// ── Tier 1: deterministic, MCP-direct ────────────────────────────
	s.run("health", s.scenarioHealth)
	s.run("mcp-tool-surface", s.scenarioToolSurface)
	s.run("cost-snapshot", s.scenarioCost)
	if *adoptSession != "" {
		s.run("adopt-observe-remove", func() error { return s.scenarioAdopt(*adoptSession) })
	} else {
		fmt.Println("SKIP adopt-observe-remove (no -adopt-session)")
	}
	s.run("journal-replay-fidelity", s.scenarioReplayFidelity)
	if *spawnDir != "" {
		s.run("spawn-direct-remove", func() error { return s.scenarioSpawnDirect(*spawnDir) })
	} else {
		fmt.Println("SKIP spawn-direct-remove (no -spawn-dir)")
	}

	// ── Tier 2: overseer-mediated (prompt ↔ tool contract) ───────────
	if !*skipOverseer {
		s.run("chat-round-trip", func() error {
			_, err := s.chatTurn("Reply with exactly: suite-ping", 180*time.Second)
			return err
		})
		s.run("context-recall", s.scenarioRecall)
		s.run("overseer-tools-live", s.scenarioOverseerTools)
		s.run("inflight-collision-frame", s.scenarioCollision)
		if *enableRewind {
			s.run("rewind-journal-event", s.scenarioRewind)
		} else {
			fmt.Println("SKIP rewind-journal-event (no -enable-rewind)")
		}
	}
	if *restartCmd != "" {
		s.run("restart-durability", func() error { return s.scenarioRestart(*restartCmd) })
	} else {
		fmt.Println("SKIP restart-durability (no -restart-cmd)")
	}

	if s.failures > 0 {
		fmt.Printf("FAIL: %d scenario(s) failed\n", s.failures)
		os.Exit(1)
	}
	fmt.Println("PASS: all executed scenarios green")
}

func (s *suite) run(name string, fn func() error) {
	start := time.Now()
	if err := fn(); err != nil {
		s.failures++
		fmt.Printf("FAIL %-24s %v (%s)\n", name, err, time.Since(start).Round(time.Millisecond))
		return
	}
	fmt.Printf("ok   %-24s (%s)\n", name, time.Since(start).Round(time.Millisecond))
}

// ── Tier 1 scenarios ─────────────────────────────────────────────────

func (s *suite) scenarioHealth() error {
	var out struct {
		Status string `json:"status"`
	}
	if err := s.getJSON("/health", &out); err != nil {
		return err
	}
	if out.Status != "ok" {
		return fmt.Errorf("status = %q", out.Status)
	}
	return nil
}

func (s *suite) scenarioToolSurface() error {
	res, err := s.mcp("tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var out struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return fmt.Errorf("decode tools/list: %w", err)
	}
	have := map[string]bool{}
	for _, t := range out.Tools {
		have[t.Name] = true
	}
	for _, want := range []string{
		"jevons_thread_adopt", "jevons_thread_list", "jevons_thread_direct",
		"jevons_thread_spawn", "jevons_thread_remove", "jwork", "jevons_cost",
		"jevons_plan_usage",
		"jevons_agent_send",
	} {
		if !have[want] {
			return fmt.Errorf("tool %s missing (got %d tools)", want, len(out.Tools))
		}
	}
	return nil
}

func (s *suite) scenarioCost() error {
	var out map[string]any
	if err := s.getJSON("/api/cost", &out); err != nil {
		return err
	}
	if len(out) == 0 {
		return fmt.Errorf("empty cost snapshot")
	}
	return nil
}

func (s *suite) scenarioAdopt(sessionID string) error {
	res, err := s.mcp("tools/call", map[string]any{
		"name": "jevons_thread_adopt",
		"arguments": map[string]any{
			"session_id": sessionID, "observe_only": true, "id": "live-suite-adoptee",
		},
	})
	if err != nil {
		return fmt.Errorf("adopt: %w", err)
	}
	if txt := mcpText(res); strings.Contains(strings.ToLower(txt), "error") {
		return fmt.Errorf("adopt result: %s", txt)
	}
	// Ground truth: the thread store lists it.
	list, err := s.mcp("tools/call", map[string]any{"name": "jevons_thread_list", "arguments": map[string]any{}})
	if err != nil {
		return err
	}
	if !strings.Contains(mcpText(list), "live-suite-adoptee") {
		return fmt.Errorf("adopted thread not in list")
	}
	// Clean up: remove the record (the on-disk session is untouched).
	if _, err := s.mcp("tools/call", map[string]any{
		"name": "jevons_thread_remove", "arguments": map[string]any{"id": "live-suite-adoptee"},
	}); err != nil {
		return fmt.Errorf("cleanup remove: %w", err)
	}
	return nil
}

// scenarioReplayFidelity: what a reconnecting client replays must equal
// the journal on disk (🎯T30.1's UI-facing guarantee, checked byte-wise
// on the first and last lines plus the count).
func (s *suite) scenarioReplayFidelity() error {
	path := filepath.Join(s.stateDir, "chatlog", s.overseer+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	var disk []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			disk = append(disk, sc.Text())
		}
	}
	if len(disk) == 0 {
		return fmt.Errorf("journal empty — nothing to verify")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+s.host+"/ws/chat", nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)

	var replayed []string
	for len(replayed) < len(disk) {
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		_, data, err := conn.Read(rctx)
		rcancel()
		if err != nil {
			break // silence — replay finished (live frames may follow later)
		}
		replayed = append(replayed, string(data))
	}
	if len(replayed) < len(disk) {
		return fmt.Errorf("replayed %d lines, journal has %d", len(replayed), len(disk))
	}
	if replayed[0] != disk[0] || replayed[len(disk)-1] != disk[len(disk)-1] {
		return fmt.Errorf("replayed boundary lines differ from journal")
	}
	return nil
}

func (s *suite) scenarioSpawnDirect(workdir string) error {
	id := "live-suite-worker"
	if _, err := s.mcp("tools/call", map[string]any{
		"name":      "jevons_thread_spawn",
		"arguments": map[string]any{"id": id, "workdir": workdir, "description": "live-suite spawn scenario"},
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	defer s.mcp("tools/call", map[string]any{
		"name": "jevons_thread_remove", "arguments": map[string]any{"id": id},
	})
	res, err := s.mcp("tools/call", map[string]any{
		"name":      "jevons_thread_direct",
		"arguments": map[string]any{"id": id, "text": "Reply with exactly: spawned-ok"},
	})
	if err != nil {
		return fmt.Errorf("direct: %w", err)
	}
	// Normalize before matching: models echo "spawned-ok" with stray
	// spaces/hyphens; the oracle is delivery, not typography.
	norm := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\n", "").Replace(mcpText(res)))
	if !strings.Contains(norm, "spawnedok") {
		return fmt.Errorf("direct reply: %s", trim(mcpText(res), 100))
	}
	return nil
}

// scenarioCollision: a second prompt while one is in flight must surface
// a wire error frame ("message not delivered"), never vanish silently
// (🎯T49's fix; found live in the T30.1 drill).
func (s *suite) scenarioCollision() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+s.host+"/ws/chat", nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	frames := make(chan []byte, 256)
	go func() {
		defer close(frames)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			frames <- data
		}
	}()
drain:
	for {
		select {
		case <-frames:
		case <-time.After(700 * time.Millisecond):
			break drain
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte("Count slowly from 1 to 5, one number per line.")); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond) // land inside the first turn
	if err := conn.Write(ctx, websocket.MessageText, []byte("collision probe")); err != nil {
		return err
	}
	deadline := time.After(90 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("connection closed before error frame")
			}
			var m struct {
				Type  string `json:"type"`
				Error string `json:"error"`
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "error" &&
				strings.Contains(m.Error, "not delivered") {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("no 'message not delivered' frame within 90s")
		}
	}
}

// scenarioRewind: a rewind control frame must broadcast a "rewound"
// frame (live-only), SHRINK the journal by the trimmed turn, and leave
// replay consistent with the truncated journal (🎯T52 semantics: the
// journal is truncated, so journaling the frame too would double-trim
// replayed views).
func (s *suite) scenarioRewind() error {
	path := filepath.Join(s.stateDir, "chatlog", s.overseer+".jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+s.host+"/ws/chat", nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	frames := make(chan []byte, 256)
	go func() {
		defer close(frames)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			frames <- data
		}
	}()
drain:
	for {
		select {
		case <-frames:
		case <-time.After(700 * time.Millisecond):
			break drain
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"rewind","turns":1}`)); err != nil {
		return err
	}
	deadline := time.After(90 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("connection closed before rewound frame")
			}
			if strings.Contains(string(data), `"rewound"`) {
				after, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if len(after) >= len(before) {
					return fmt.Errorf("journal did not shrink (%d -> %d bytes)", len(before), len(after))
				}
				if bytes.Contains(after, []byte(`"rewound"`)) {
					return fmt.Errorf("rewound frame journaled — would double-trim replay")
				}
				return s.scenarioReplayFidelity()
			}
		case <-deadline:
			return fmt.Errorf("no rewound frame within 90s")
		}
	}
}

// scenarioRestart: kill/restart the daemon via restartCmd, then require
// journal replay intact plus a context-coherent reply (🎯T30.1 live).
func (s *suite) scenarioRestart(restartCmd string) error {
	if _, err := s.chatTurn("Reply with exactly: pre-restart-marker", 180*time.Second); err != nil {
		return fmt.Errorf("pre-restart turn: %w", err)
	}
	cmd := exec.Command("/bin/sh", "-c", restartCmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restart cmd: %v (%s)", err, trim(string(out), 120))
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon did not come back within 60s")
		}
		var out struct {
			Status string `json:"status"`
		}
		if err := s.getJSON("/health", &out); err == nil && out.Status == "ok" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	time.Sleep(10 * time.Second) // overseer attach + recap settle
	if err := s.scenarioReplayFidelity(); err != nil {
		return fmt.Errorf("post-restart replay: %w", err)
	}
	reply, err := s.chatTurn("What exact hyphenated marker did I ask you to reply with just before your restart? Only the marker.", 240*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(reply, "pre-restart-marker") {
		return fmt.Errorf("post-restart recall %q — recap continuity failed", trim(reply, 80))
	}
	return nil
}

// ── Tier 2 scenarios ─────────────────────────────────────────────────

func (s *suite) scenarioRecall() error {
	if _, err := s.chatTurn("Reply with exactly: suite-recall-token", 180*time.Second); err != nil {
		return err
	}
	reply, err := s.chatTurn("What exact hyphenated token did I just ask you to reply with? Answer with only that token.", 180*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(reply, "suite-recall-token") {
		return fmt.Errorf("recall reply %q lacks the token", trim(reply, 80))
	}
	return nil
}

func (s *suite) scenarioOverseerTools() error {
	reply, err := s.chatTurn(
		"Call "+s.ns+"__jevons_thread_list via use_tool now. If the call succeeds, reply with only the first word of its raw output (it will be THREAD); if it fails, reply NO-TOOLS plus the error verbatim.",
		240*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToUpper(reply), "THREAD") {
		return fmt.Errorf("overseer tool reply %q — tools not attached?", trim(reply, 80))
	}
	return nil
}

var errInFlight = fmt.Errorf("prompt already in flight")

// chatTurn connects, drains the journal replay, sends one prompt, and
// returns the coalesced assistant reply for the live turn. Boot and
// rewind inject recap turns asynchronously, so an in-flight collision
// is expected congestion, not failure — retry with backoff.
func (s *suite) chatTurn(prompt string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		reply, err := s.chatTurnOnce(prompt, timeout)
		if err != errInFlight {
			return reply, err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("overseer stayed busy for %s", timeout)
		}
		time.Sleep(4 * time.Second)
	}
}

func (s *suite) chatTurnOnce(prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+s.host+"/ws/chat", nil)
	if err != nil {
		return "", err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)

	frames := make(chan []byte, 256)
	go func() {
		defer close(frames)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			frames <- data
		}
	}()

	// Drain replay: 700ms of silence = done (see scripts/chat-smoke).
drain:
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				return "", fmt.Errorf("connection closed during replay drain")
			}
		case <-time.After(700 * time.Millisecond):
			break drain
		}
	}

	if err := conn.Write(ctx, websocket.MessageText, []byte(prompt)); err != nil {
		return "", err
	}

	var reply strings.Builder
	for {
		data, ok := <-frames
		if !ok {
			return "", fmt.Errorf("connection closed mid-turn (got %q)", trim(reply.String(), 60))
		}
		var m struct {
			Type    string `json:"type"`
			Error   string `json:"error"`
			Message struct {
				StopReason string `json:"stop_reason"`
				Content    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Type == "error" {
			if strings.Contains(m.Error, "already in flight") {
				return "", errInFlight
			}
			return "", fmt.Errorf("wire error: %s", m.Error)
		}
		if m.Type != "assistant" {
			continue
		}
		for _, c := range m.Message.Content {
			reply.WriteString(c.Text)
		}
		if m.Message.StopReason == "end_turn" || m.Message.StopReason == "stop_sequence" {
			return reply.String(), nil
		}
	}
}

// ── plumbing ─────────────────────────────────────────────────────────

func (s *suite) getJSON(path string, out any) error {
	resp, err := http.Get("http://" + s.host + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *suite) mcp(method string, params any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	req, _ := http.NewRequest("POST", "http://"+s.host+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s decode: %w", method, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", method, out.Error.Message)
	}
	return out.Result, nil
}

func mcpText(res json.RawMessage) string {
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(res, &out)
	var b strings.Builder
	for _, c := range out.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func trim(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
