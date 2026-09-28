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

// This is a hermetic outage control, not a live-agent journey. A real daemon
// boots against a real Claudia broker whose provider sidecar withholds its
// answer to adopt or load. The control proves fleet reads and shutdown still
// work during that startup state, and that the saved session identity is not
// reminted.
//
// Every provider a seat can use now runs on the Oh My Pi sidecar behind the
// broker, and only the broker holds the plan credentials, so a direct-process
// fixture (CURSOR_BIN, CODEX_BIN) no longer reaches a daemon's startup at all
// (🎯T882). The stall is therefore placed where production would meet it: a
// fake bun sidecar the broker starts and then waits on.
func TestT627StartupShutdownControl(t *testing.T) {
	if _, ok := any(&claudia.Registry{}).(interface{ StartAllPreferAdoptContext(context.Context) }); !ok {
		t.Skip("published Claudia predates contextual startup; run this control with the local dependency workspace")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal control")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python is required for the hermetic sidecar fixture")
	}
	bin := buildJevonsdT526(t)
	broker := filepath.Join(t.TempDir(), "claudia")
	if out, err := exec.Command("go", "build", "-o", broker, "github.com/marcelocantos/claudia/cmd/claudia").CombinedOutput(); err != nil {
		t.Fatalf("go build claudia: %v\n%s", err, out)
	}
	// The broker tries adopt first; a sidecar without the seat refuses it
	// and the broker falls back to load. Either can stall.
	for _, op := range []string{"adopt", "load"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP} {
			t.Run(op+"/"+sig.String(), func(t *testing.T) {
				runT627StartupShutdownCase(t, bin, broker, python, op, sig)
			})
		}
	}
}

const t627Sidecar = `import json, os, socket, sys, threading
path = sys.argv[-1]
withhold = os.environ["FAKE_SIDECAR_WITHHOLD"]
try:
    os.unlink(path)
except FileNotFoundError:
    pass
srv = socket.socket(socket.AF_UNIX)
srv.bind(path)
srv.listen(16)
def serve(conn):
    r, w = conn.makefile("r"), conn.makefile("w")
    for line in r:
        msg = json.loads(line)
        op = msg.get("op")
        if op == withhold:
            with open(os.environ["STARTUP_MARKER"], "w") as f:
                json.dump({"pid": os.getpid(), "method": op, "seat": msg.get("seat", "")}, f)
            threading.Event().wait()
        if op == "adopt":
            w.write(json.dumps({"seat": msg.get("seat"), "type": "error", "text": "no such seat"}) + "\n")
            w.flush()
        elif op == "load":
            w.write(json.dumps({"seat": msg.get("seat"), "type": "ready"}) + "\n")
            w.flush()
while True:
    conn, _ = srv.accept()
    threading.Thread(target=serve, args=(conn,), daemon=True).start()
`

func runT627StartupShutdownCase(t *testing.T, bin, broker, python, withhold string, sig syscall.Signal) {
	t.Helper()
	root := t.TempDir()
	state, home, fakeBin, xdg := filepath.Join(root, "state"), filepath.Join(root, "home"), filepath.Join(root, "bin"), filepath.Join(root, "xdg")
	spool := filepath.Join(home, ".jevons", "spool")
	for _, dir := range []string{state, home, fakeBin, xdg, spool} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Unix socket paths are capped near 104 bytes; a test temp dir is not.
	sockDir, err := os.MkdirTemp("/tmp", "t627")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	// Prevent this fixture from inspecting/reaping the owner's processes or
	// controlling the owner's launchd jobs. security hands the broker one
	// plan record so no login is attempted; it never touches a Keychain.
	for name, code := range map[string]string{"ps": "0", "launchctl": "0", "lsof": "1", "true": "0"} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte("#!/bin/sh\nexit "+code+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	security := "#!/bin/sh\ncase \"$*\" in\n  *find-generic-password*) printf '%s\\n' '" +
		`{"records":{"xai-oauth":{"refresh_token":"","access_token":"fixture-token","expiry":"2099-01-01T00:00:00Z"}}}` +
		"'; exit 0;;\n  *) exit 0;;\nesac\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "security"), []byte(security), 0700); err != nil {
		t.Fatal(err)
	}
	py := filepath.Join(root, "sidecar.py")
	if err := os.WriteFile(py, []byte(t627Sidecar), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "bun"), []byte(fmt.Sprintf("#!/bin/sh\nexec %q %q \"$@\"\n", python, py)), 0700); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(root, "server.ts")
	if err := os.WriteFile(server, nil, 0600); err != nil {
		t.Fatal(err)
	}
	const sid = "saved-startup-fixture"
	// A resumed sidecar seat must have spool history, or Claudia refuses to
	// start it rather than mint a replacement (🎯T869).
	rec := `{"ts":"2026-09-28T00:00:00Z","seat":"startup-fixture","type":"text","text":"an earlier turn"}` + "\n"
	if err := os.WriteFile(filepath.Join(spool, "events-2026-09-28.log"), []byte(rec), 0600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := filepath.Join(root, "config.yaml")
	body := fmt.Sprintf("owner_name: Test\noverseer_name: startup-fixture\nstate_dir: %q\nworkdir: %q\nprovider: grok\nmcp_server_name: startup-fixture\nfrontier_consume:\n  disabled: true\n", state, root)
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	trueBin := filepath.Join(fakeBin, "true")
	defs, _ := json.Marshal([]claudia.AgentDef{{
		Name:         "startup-fixture",
		SessionID:    sid,
		WorkDir:      root,
		Provider:     claudia.Provider("xai-oauth"),
		Materialized: true,
		AutoStart:    true,
		Purpose:      claudia.PurposeOverseer,
		MCPExclusive: true,
		// The development overseer loads a large MCP map; the hang was that load.
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
	brokerSock := filepath.Join(sockDir, "b.sock")
	env := []string{
		"HOME=" + home,
		"PATH=" + fakeBin + ":/usr/bin:/bin",
		"XDG_STATE_HOME=" + xdg,
		"CLAUDIA_BROKER_SOCKET=" + brokerSock,
		"CLAUDIA_OMP_SOCKET=" + filepath.Join(sockDir, "o.sock"),
		"CLAUDIA_OMP_SERVER=" + server,
		"STARTUP_MARKER=" + marker,
		"FAKE_SIDECAR_WITHHOLD=" + withhold,
	}
	start := func(name string, args ...string) (*exec.Cmd, chan error, string) {
		t.Helper()
		logPath := filepath.Join(root, filepath.Base(name)+".log")
		logs, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { logs.Close() })
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = root, env, logs, logs
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		return cmd, done, logPath
	}
	brokerCmd, brokerDone, brokerLog := start(broker, "broker", "serve")
	t.Cleanup(func() {
		_ = brokerCmd.Process.Kill()
		<-brokerDone
	})
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, err := net.Dial("unix", brokerSock); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(brokerLog)
			t.Fatalf("broker did not start: %s", b)
		}
	}
	cmd, done, logPath := start(bin, "-config", cfg, "-port", fmt.Sprint(port), "-bind", "127.0.0.1", "-workdir", root)
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
		PID    int
		Method string
		Seat   string
	}
waitLoad:
	for {
		select {
		case err := <-done:
			finished = true
			t.Fatalf("daemon exited before blocked startup: %v", err)
		case <-deadline.C:
			b, _ := os.ReadFile(logPath)
			bb, _ := os.ReadFile(brokerLog)
			t.Fatalf("did not reach %s:\n%s\nbroker:\n%s", withhold, b, bb)
		case <-tick.C:
			b, _ := os.ReadFile(marker)
			if json.Unmarshal(b, &loaded) == nil && loaded.PID > 0 && loaded.Method == withhold {
				break waitLoad
			}
		}
	}
	// The sidecar is detached by design and outlives both daemons; the
	// fixture owns this one and must not leak it.
	t.Cleanup(func() {
		if p, err := os.FindProcess(loaded.PID); err == nil {
			_ = p.Kill()
		}
	})
	if loaded.Seat != "startup-fixture" {
		t.Fatalf("startup stalled on the wrong seat: %+v", loaded)
	}
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
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if def := reg.Def("startup-fixture"); def == nil || def.SessionID != sid {
		t.Fatalf("shutdown lost saved identity: %+v", def)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// The broker owns the seat, so neither exit may stop it (🎯T63).
	wantMode := "exit_mode=normal stop_agents=false"
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
	// A broker-owned seat is left running on either exit, so both write the
	// handoff the next boot reattaches from, and it must keep the identity.
	if snap == nil || len(snap.Agents) != 1 || snap.Agents[0].Name != "startup-fixture" || snap.Agents[0].SessionID != sid {
		t.Fatalf("exit handoff lost saved identity: %+v", snap)
	}
}
