// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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
	root := t.TempDir()
	state, home := filepath.Join(root, "state"), filepath.Join(root, "home")
	for _, dir := range []string{state, home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	wantSID := t63PrimeSeat(t, name, root, provider)
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
	t63WaitUnowned(t, name)

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
	if err := reclaimed.Send("Reply with exactly: pong"); err != nil {
		t.Fatalf("reclaim Send: %v", err)
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

// TestT63PrimeSeat is a subprocess helper: grant a daemon seat and exit
// without Stop so the seat stays held and unowned for jevonsd to reclaim.
func TestT63PrimeSeat(t *testing.T) {
	if os.Getenv("T63_PRIME") != "1" {
		t.Skip("helper for TestT63DaemonReclaimJourney")
	}
	t.Setenv("CLAUDIA_NO_BROKER", "")
	a, err := claudia.Start(claudia.Config{
		Name:     os.Getenv("T63_PRIME_NAME"),
		Provider: claudia.Provider(os.Getenv("T63_PRIME_PROVIDER")),
		WorkDir:  os.Getenv("T63_PRIME_WORKDIR"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "prime Start: %v\n", err)
		os.Exit(1)
	}
	if err := a.WaitReady(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "prime WaitReady: %v\n", err)
		os.Exit(1)
	}
	if err := a.Send("Reply with exactly: pong"); err != nil {
		fmt.Fprintf(os.Stderr, "prime Send: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("T63_PRIME_OUT"), []byte(a.SessionID()), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "prime write sid: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func t63PrimeSeat(t *testing.T, name, workdir string, provider claudia.Provider) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "sid")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestT63PrimeSeat$", "-test.v=false")
	cmd.Env = append(os.Environ(),
		"T63_PRIME=1",
		"T63_PRIME_NAME="+name,
		"T63_PRIME_WORKDIR="+workdir,
		"T63_PRIME_PROVIDER="+string(provider),
		"T63_PRIME_OUT="+outPath,
		"CLAUDIA_NO_BROKER=",
	)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prime seat: %v\n%s", err, raw)
	}
	sid, err := os.ReadFile(outPath)
	if err != nil || strings.TrimSpace(string(sid)) == "" {
		t.Fatalf("prime seat wrote no session id: %v", err)
	}
	return strings.TrimSpace(string(sid))
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

func t63WaitUnowned(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		owned, alive, ok := t63GrantFlags(t, name)
		if ok && alive && !owned {
			return
		}
		if ok && !alive {
			t.Fatalf("seat %s died before reclaim", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
	owned, alive, ok := t63GrantFlags(t, name)
	t.Fatalf("seat %s still owned after SIGHUP (ok=%v owned=%v alive=%v)", name, ok, owned, alive)
}

func t63GrantFlags(t *testing.T, name string) (owned, alive, ok bool) {
	t.Helper()
	for _, line := range strings.Split(t63GrantsOutput(t), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != name {
			continue
		}
		for i := 2; i < len(f)-1; i++ {
			if (f[i] == "true" || f[i] == "false") && (f[i+1] == "true" || f[i+1] == "false") {
				return f[i] == "true", f[i+1] == "true", true
			}
		}
	}
	return false, false, false
}

func t63GrantCount(t *testing.T, name string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(t63GrantsOutput(t), "\n") {
		if strings.HasPrefix(line, name+"\t") || strings.HasPrefix(line, name+" ") {
			n++
		}
	}
	return n
}

func t63GrantsOutput(t *testing.T) string {
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
	return string(out)
}
