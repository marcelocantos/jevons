// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// TestT63DaemonReclaimJourney is 🎯T63: with the host claudia daemon
// holding seats, jevonsd starts a worker, is restarted with SIGTERM and
// SIGHUP, and ReattachFleet reclaims the same session. The daemon grant
// table stays at one seat. Run via `make t63-daemon-reclaim` (clean
// worktree, published claudia module).
func TestT63DaemonReclaimJourney(t *testing.T) {
	if os.Getenv("CLAUDIA_NO_BROKER") != "" {
		t.Setenv("CLAUDIA_NO_BROKER", "")
	}
	if !claudia.BrokerAvailable() {
		t.Skip("no claudia daemon (brew services start claudia; unset CLAUDIA_NO_BROKER)")
	}
	provider, gate := t63LiveProvider(t)
	name := "t63-" + string(provider) + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	wantSID := uuid.NewString()
	root := t.TempDir()
	state, home := filepath.Join(root, "state"), filepath.Join(root, "home")
	for _, dir := range []string{state, home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(root, "config.yaml")
	body := fmt.Sprintf("owner_name: T63\noverseer_name: %s\nstate_dir: %q\nworkdir: %q\nprovider: %s\nmcp_server_name: t63-journey\nfrontier_consume:\n  disabled: true\n", name, state, root, provider)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	defs, err := json.Marshal([]claudia.AgentDef{{
		Name: name, WorkDir: root, Provider: provider, SessionID: wantSID,
		AutoStart: true, Purpose: claudia.PurposeWork,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agents.json"), defs, 0o600); err != nil {
		t.Fatal(err)
	}

	bin := buildJevonsdT526(t)
	port := t63FreePort(t)
	sock := os.Getenv("CLAUDIA_BROKER_SOCKET")
	if sock == "" {
		sock = filepath.Join(os.Getenv("HOME"), ".local/state/claudia/broker.sock")
	}

	logPath := filepath.Join(root, "jevonsd.log")
	start := func() (*exec.Cmd, chan error) {
		logs, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = logs.Close() })
		cmd := exec.Command(bin, "-config", cfg, "-port", fmt.Sprint(port), "-bind", "127.0.0.1", "-workdir", root)
		cmd.Dir = root
		cmd.Env = t63IsolateEnv(home, sock)
		cmd.Stdout, cmd.Stderr = logs, logs
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		return cmd, done
	}
	waitRunning := func(done <-chan error) string {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case err := <-done:
				raw, _ := os.ReadFile(logPath)
				t.Fatalf("jevonsd exited before the worker came up: %v\n%s", err, raw)
			default:
			}
			if snap, err := upgrade.SessionSnapshotFromFile(filepath.Join(state, "agents.json")); err == nil {
				if sid := snap[name]; sid != "" && t63AgentRunning(t, port, name) {
					return sid
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		raw, _ := os.ReadFile(logPath)
		t.Fatalf("worker %s did not come up on %s (gate %s)\n%s", name, provider, gate, raw)
		return ""
	}
	stop := func(cmd *exec.Cmd, done <-chan error, sig syscall.Signal) {
		t.Helper()
		if err := cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Fatalf("%s did not exit", sig)
		}
	}

	first, firstDone := start()
	sid := waitRunning(firstDone)
	if sid != wantSID {
		t.Fatalf("first boot reminted session: %s → %s", wantSID, sid)
	}
	if n := t63GrantCount(t, name); n != 1 {
		t.Fatalf("after start: daemon grants for %s = %d, want 1", name, n)
	}
	t63Send(t, port, name, "Reply with exactly: pong")

	stop(first, firstDone, syscall.SIGTERM)
	if n := t63GrantCount(t, name); n != 1 {
		t.Fatalf("after SIGTERM: daemon grants for %s = %d, want 1 (StopAll released the seat)", name, n)
	}
	if snap, _ := upgrade.SessionSnapshotFromFile(filepath.Join(state, "agents.json")); snap[name] != sid {
		t.Fatalf("SIGTERM reminted session: %s → %s", sid, snap[name])
	}

	second, secondDone := start()
	if got := waitRunning(secondDone); got != sid {
		t.Fatalf("after SIGTERM restart: session %s → %s", sid, got)
	}
	if n := t63GrantCount(t, name); n != 1 {
		t.Fatalf("after SIGTERM restart: grants = %d, want 1", n)
	}

	stop(second, secondDone, syscall.SIGHUP)
	if n := t63GrantCount(t, name); n != 1 {
		t.Fatalf("after SIGHUP: daemon grants for %s = %d, want 1", name, n)
	}

	third, thirdDone := start()
	if got := waitRunning(thirdDone); got != sid {
		t.Fatalf("after SIGHUP restart: session %s → %s", sid, got)
	}
	stop(third, thirdDone, syscall.SIGHUP)

	t.Setenv("CLAUDIA_BROKER_SOCKET", sock)
	t.Setenv("CLAUDIA_NO_BROKER", "")
	reclaimed, err := claudia.Start(claudia.Config{
		Name: name, Provider: provider, WorkDir: root, SessionID: sid,
	})
	if err != nil {
		t.Fatalf("reclaim Start: %v", err)
	}
	t.Cleanup(reclaimed.Stop)
	if reclaimed.SessionID() != sid {
		t.Fatalf("reclaim reminted: %s → %s", sid, reclaimed.SessionID())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	text, err := reclaimed.WaitForResponse(ctx)
	if err != nil {
		t.Fatalf("WaitForResponse after reclaim: %v", err)
	}
	if !strings.Contains(strings.ToLower(text), "pong") {
		t.Fatalf("reclaimed turn = %q, want pong", text)
	}
	if n := t63GrantCount(t, name); n != 1 {
		t.Fatalf("after reclaim WaitForResponse: grants = %d, want 1", n)
	}
}

func t63LiveProvider(t *testing.T) (claudia.Provider, string) {
	t.Helper()
	for _, tc := range []struct {
		gate     string
		provider claudia.Provider
	}{
		{"CLAUDIA_GROK_LIVE", claudia.ProviderGrok},
		{"CLAUDIA_CURSOR_LIVE", claudia.ProviderCursor},
		{"CLAUDIA_LIVE", claudia.ProviderClaude},
		{"CLAUDIA_CODEX_LIVE", claudia.ProviderCodex},
	} {
		if os.Getenv(tc.gate) != "" {
			return tc.provider, tc.gate
		}
	}
	t.Skip("set CLAUDIA_GROK_LIVE=1 (or CURSOR/LIVE/CODEX) — this journey spends plan capacity")
	return "", ""
}

func t63FreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func t63IsolateEnv(home, sock string) []string {
	out := []string{"HOME=" + home, "CLAUDIA_BROKER_SOCKET=" + sock, "CLAUDIA_NO_BROKER="}
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "HOME="),
			strings.HasPrefix(kv, "CLAUDIA_NO_BROKER="),
			strings.HasPrefix(kv, "CLAUDIA_BROKER_SOCKET="):
			continue
		default:
			out = append(out, kv)
		}
	}
	return out
}

func t63AgentRunning(t *testing.T, port int, name string) bool {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/agents", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var rows []struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
	}
	if json.NewDecoder(resp.Body).Decode(&rows) != nil {
		return false
	}
	for _, r := range rows {
		if r.Name == name && r.Running {
			return true
		}
	}
	return false
}

func t63Send(t *testing.T, port int, name, text string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"text": text})
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/api/agents/%s/send", port, name), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("send status %d: %s", resp.StatusCode, raw)
	}
}

func t63GrantCount(t *testing.T, name string) int {
	t.Helper()
	bin := "/opt/homebrew/bin/claudia"
	if _, err := os.Stat(bin); err != nil {
		var err2 error
		bin, err2 = exec.LookPath("claudia")
		if err2 != nil {
			t.Fatal("claudia CLI not found")
		}
	}
	out, err := exec.Command(bin, "broker", "grants").CombinedOutput()
	if err != nil {
		t.Fatalf("claudia broker grants: %v\n%s", err, out)
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, name+"\t") || strings.HasPrefix(line, name+" ") {
			n++
		}
	}
	return n
}
