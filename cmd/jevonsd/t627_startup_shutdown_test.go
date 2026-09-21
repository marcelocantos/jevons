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
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

func TestT627BootInstallsShutdownBeforeReattach(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	notify := strings.Index(body, "signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)")
	shutdown := strings.Index(body, `slog.Info("shutting down"`)
	reattach := strings.Index(body, "upgrade.ReattachSeatsContext(ctx, registry, isOverseerSeat, 1)")
	if notify < 0 || shutdown < 0 || reattach < 0 {
		t.Fatal("boot lost shutdown or reattach markers")
	}
	if !(notify < shutdown && shutdown < reattach) {
		t.Fatalf("shutdown handling is not installed before fleet recovery: notify=%d shutdown=%d reattach=%d", notify, shutdown, reattach)
	}
}

// This is a hermetic outage control, not a live-agent journey. The provider
// reaches a real daemon's initialize or session/load and withholds the answer.
// The control proves fleet reads and shutdown still work during that startup
// state, and that the saved session identity is not reminted.
func TestT627StartupShutdownControl(t *testing.T) {
	if _, ok := any(&claudia.Registry{}).(interface{ StartAllPreferAdoptContext(context.Context) }); !ok {
		t.Skip("published Claudia predates contextual startup; run this control with the local dependency workspace")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal control")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python is required for the hermetic ACP fixture")
	}
	bin := buildJevonsdT526(t)
	for _, method := range []string{"initialize", "session/load"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP} {
			t.Run(method+"/"+sig.String(), func(t *testing.T) {
				runT627StartupShutdownCase(t, bin, python, method, sig)
			})
		}
	}
}

func runT627StartupShutdownCase(t *testing.T, bin, python, withhold string, sig syscall.Signal) {
	t.Helper()
	root := t.TempDir()
	state, home, fakeBin := filepath.Join(root, "state"), filepath.Join(root, "home"), filepath.Join(root, "bin")
	for _, dir := range []string{state, home, fakeBin} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Prevent this fixture from inspecting/reaping the owner's processes,
	// contacting Keychain, or controlling the owner's launchd jobs.
	for name, code := range map[string]string{"ps": "0", "launchctl": "0", "security": "1", "lsof": "1", "true": "0"} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte("#!/bin/sh\nexit "+code+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	provider := filepath.Join(fakeBin, "cursor-fixture")
	fixture := `import json, os, sys, threading
withhold = os.environ.get("FAKE_ACP_WITHHOLD", "session/load")
for line in sys.stdin:
    msg = json.loads(line)
    method = msg.get("method")
    if method == withhold:
        params = msg.get("params") or {}
        with open(os.environ["STARTUP_MARKER"], "w") as f:
            json.dump({"pid": os.getpid(), "session": params.get("sessionId", ""), "method": method}, f)
        threading.Event().wait()
    if "id" not in msg:
        continue
    result = {"protocolVersion": 1, "agentCapabilities": {"loadSession": True}, "authMethods": [{"id":"cursor_login"}]} if method == "initialize" else {}
    print(json.dumps({"jsonrpc":"2.0", "id":msg["id"], "result":result}), flush=True)
`
	py := filepath.Join(root, "provider.py")
	if err := os.WriteFile(py, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider, []byte(fmt.Sprintf("#!/bin/sh\nexec %q %q\n", python, py)), 0700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := filepath.Join(root, "config.yaml")
	body := fmt.Sprintf("owner_name: Test\noverseer_name: startup-fixture\nstate_dir: %q\nworkdir: %q\nprovider: cursor\nmcp_server_name: startup-fixture\nfrontier_consume:\n  disabled: true\n", state, root)
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	const sid = "saved-startup-fixture"
	trueBin := filepath.Join(fakeBin, "true")
	defs, _ := json.Marshal([]claudia.AgentDef{{
		Name:         "startup-fixture",
		SessionID:    sid,
		WorkDir:      root,
		Provider:     claudia.ProviderCursor,
		Materialized: true,
		AutoStart:    true,
		Purpose:      claudia.PurposeOverseer,
		MCPExclusive: true,
		// Development Cursor loads a large MCP map; the hang was that load.
		// Names match the daily shape; commands stay inside this fixture.
		MCPServers: []claudia.MCPServer{
			{Name: "jevonsmcp", Type: "http", URL: fmt.Sprintf("http://127.0.0.1:%d/mcp", port)},
			{Name: "bullseye", Command: trueBin},
			{Name: "playwright", Command: trueBin},
			{Name: "mnemo", Command: trueBin},
		},
	}})
	if err := os.WriteFile(filepath.Join(state, "agents.json"), defs, 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "startup.json")
	logs, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	cmd := exec.Command(bin, "-config", cfg, "-port", fmt.Sprint(port), "-bind", "127.0.0.1", "-workdir", root)
	cmd.Dir = root
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + fakeBin + ":/usr/bin:/bin",
		"CURSOR_BIN=" + provider,
		"STARTUP_MARKER=" + marker,
		"FAKE_ACP_WITHHOLD=" + withhold,
		"CLAUDIA_NO_BROKER=1",
	}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = cmd.Process.Kill()
			<-done
		}
	})
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var loaded struct {
		PID     int
		Session string
		Method  string
	}
waitLoad:
	for {
		select {
		case err := <-done:
			finished = true
			t.Fatalf("daemon exited before blocked startup: %v", err)
		case <-deadline.C:
			b, _ := os.ReadFile(logs.Name())
			t.Fatalf("did not reach %s: %s", withhold, b)
		case <-tick.C:
			b, _ := os.ReadFile(marker)
			if json.Unmarshal(b, &loaded) == nil && loaded.PID > 0 && loaded.Method == withhold {
				break waitLoad
			}
		}
	}
	if withhold == "session/load" && loaded.Session != sid {
		t.Fatalf("startup replaced identity: %s", loaded.Session)
	}
	t.Cleanup(func() {
		if loaded.PID > 0 {
			p, _ := os.FindProcess(loaded.PID)
			_ = p.Kill()
		}
	})
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/agents", port))
	if err != nil {
		t.Fatalf("fleet reads blocked behind startup: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fleet status=%d", resp.StatusCode)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown blocked behind startup")
	}
	if err := syscall.Kill(loaded.PID, 0); err == nil {
		t.Fatalf("startup writer %d survived shutdown", loaded.PID)
	}
	loaded.PID = 0 // Already reaped; cleanup must not signal a reused PID.
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if def := reg.Def("startup-fixture"); def == nil || def.SessionID != sid {
		t.Fatalf("shutdown lost saved identity: %+v", def)
	}
	b, err := os.ReadFile(logs.Name())
	if err != nil {
		t.Fatal(err)
	}
	wantMode := "exit_mode=normal stop_agents=true"
	if sig == syscall.SIGHUP {
		wantMode = "exit_mode=upgrade stop_agents=false"
	}
	modeSeen := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, `msg="shutting down"`) && strings.Contains(line, wantMode) {
			modeSeen = true
		}
	}
	if !modeSeen {
		t.Fatalf("shutdown did not report %q", wantMode)
	}
	snap, err := upgrade.LoadSnapshot(upgrade.SnapshotPath(state))
	if err != nil {
		t.Fatal(err)
	}
	if sig == syscall.SIGHUP {
		if snap == nil || len(snap.Agents) != 1 || snap.Agents[0].Name != "startup-fixture" || snap.Agents[0].SessionID != sid {
			t.Fatalf("upgrade handoff lost saved identity: %+v", snap)
		}
	} else if snap != nil {
		t.Fatalf("normal drain left an upgrade handoff: %+v", snap)
	}
}
