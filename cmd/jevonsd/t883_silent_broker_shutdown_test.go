// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
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
)

// supervisorStopWait is supervisord's stopwaitsecs for jevonsd
// (supervisor/jevonsd.ini): past it the daemon is SIGKILLed.
const supervisorStopWait = 15 * time.Second

// t883SilentBroker accepts every connection and never answers: the wedged
// broker a loaded host or a hung broker presents.
const t883SilentBroker = `import os, socket, sys, threading
path = sys.argv[1]
try:
    os.unlink(path)
except FileNotFoundError:
    pass
srv = socket.socket(socket.AF_UNIX)
srv.bind(path)
srv.listen(64)
def hold(conn):
    for _ in conn.makefile("r"):
        pass
while True:
    conn, _ = srv.accept()
    threading.Thread(target=hold, args=(conn,), daemon=True).start()
`

// 🎯T883: a broker that accepts but never answers must not hold shutdown.
// The daemon still treats it as possibly owning seats (🎯T796: nothing is
// stopped), but it says so and exits promptly instead of re-probing it.
func TestT883SilentBrokerDoesNotHoldShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal control")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python is required for the silent broker fixture")
	}
	bin := buildJevonsdT526(t)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			runT883Case(t, bin, python, sig)
		})
	}
}

func runT883Case(t *testing.T, bin, python string, sig syscall.Signal) {
	t.Helper()
	root := t.TempDir()
	state, home, fakeBin := filepath.Join(root, "state"), filepath.Join(root, "home"), filepath.Join(root, "bin")
	for _, dir := range []string{state, home, fakeBin} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the daemon away from the owner's processes, Keychain and launchd.
	for name, code := range map[string]string{"ps": "0", "launchctl": "0", "security": "1", "lsof": "1"} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte("#!/bin/sh\nexit "+code+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Unix socket paths are capped near 104 bytes; a test temp dir is not.
	sockDir, err := os.MkdirTemp("/tmp", "t883")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "b.sock")
	py := filepath.Join(root, "broker.py")
	if err := os.WriteFile(py, []byte(t883SilentBroker), 0600); err != nil {
		t.Fatal(err)
	}
	broker := exec.Command(python, py, sock)
	if err := broker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = broker.Process.Kill()
		_ = broker.Wait()
	})
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("silent broker did not listen")
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := filepath.Join(root, "config.yaml")
	body := fmt.Sprintf("owner_name: Test\noverseer_name: silent-fixture\nstate_dir: %q\nworkdir: %q\nprovider: grok\nmcp_server_name: silent-fixture\nfrontier_consume:\n  disabled: true\n", state, root)
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	// A broker-owned overseer seat, so boot reattaches through the broker.
	defs, _ := json.Marshal([]claudia.AgentDef{{
		Name: "silent-fixture", SessionID: "saved-silent-fixture", WorkDir: root,
		Provider: claudia.Provider("xai-oauth"), Materialized: true, AutoStart: true,
		Purpose: claudia.PurposeOverseer,
	}})
	if err := os.WriteFile(filepath.Join(state, "agents.json"), defs, 0600); err != nil {
		t.Fatal(err)
	}
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
		"XDG_STATE_HOME=" + filepath.Join(root, "xdg"),
		"CLAUDIA_BROKER_SOCKET=" + sock,
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
	client := &http.Client{Timeout: 2 * time.Second}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/agents", port)); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(logs.Name())
			t.Fatalf("daemon never served behind a silent broker:\n%s", b)
		}
	}

	sent := time.Now()
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	var logged time.Duration
	for logged == 0 {
		b, _ := os.ReadFile(logs.Name())
		if strings.Contains(string(b), `msg="shutting down"`) {
			logged = time.Since(sent)
			break
		}
		if time.Since(sent) > time.Second {
			t.Fatalf("shutdown was not reported within 1s of %s:\n%s", sig, b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
	case <-time.After(supervisorStopWait - time.Since(sent)):
		t.Fatalf("still running %s after %s behind a silent broker", supervisorStopWait, sig)
	}
	t.Logf("%s: reported after %s, exited after %s", sig, logged.Round(time.Millisecond), time.Since(sent).Round(time.Millisecond))

	b, err := os.ReadFile(logs.Name())
	if err != nil {
		t.Fatal(err)
	}
	// A broker that cannot be ruled out may own the seats: nothing is stopped.
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, `msg="shutting down"`) && !strings.Contains(line, "stop_agents=false") {
			t.Fatalf("exit would stop seats a silent broker may own: %s", line)
		}
	}
}
