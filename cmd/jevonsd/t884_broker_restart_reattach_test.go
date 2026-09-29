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
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/testreap"
)

// 🎯T884: a broker restart relaunches its seats while jevonsd keeps running.
// The daemon's handles died with the old connection; it must re-attach the
// seat the new broker runs instead of showing it stopped.
func TestT884BrokerRestartReattachesRunningSeat(t *testing.T) {
	if _, ok := any(&claudia.Registry{}).(interface{ StartAllPreferAdoptContext(context.Context) }); !ok {
		t.Skip("published Claudia predates contextual startup; run this control with the local dependency workspace")
	}
	// claudia v0.42.0's broker rejects anthropic / xai-oauth (no Oh My Pi
	// sidecar). The fixture's seat is one of those ids.
	t.Skip("published claudia does not launch Oh My Pi sidecar providers")
	if runtime.GOOS == "windows" {
		t.Skip("POSIX unix-socket control")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python is required for the hermetic sidecar fixture")
	}
	// 🎯T932: the broker detaches its seats' sidecars, so killing the broker
	// strands them, and -timeout runs no cleanup at all. Arm sweeps every
	// process naming this test's temp dirs however the binary ends.
	testreap.Arm(t)
	bin := buildJevonsdT526(t)
	brokerBin := filepath.Join(t.TempDir(), "claudia")
	if out, err := exec.Command("go", "build", "-o", brokerBin, "github.com/marcelocantos/claudia/cmd/claudia").CombinedOutput(); err != nil {
		t.Fatalf("go build claudia: %v\n%s", err, out)
	}

	root := t.TempDir()
	state, home, fakeBin, xdg := filepath.Join(root, "state"), filepath.Join(root, "home"), filepath.Join(root, "bin"), filepath.Join(root, "xdg")
	spool := filepath.Join(home, ".jevons", "spool")
	for _, dir := range []string{state, home, fakeBin, xdg, spool} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	sockDir, err := os.MkdirTemp("/tmp", "t884")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	for name, code := range map[string]string{"ps": "0", "launchctl": "0", "lsof": "1"} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte("#!/bin/sh\nexit "+code+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	security := "#!/bin/sh\ncase \"$*\" in\n  *find-generic-password*) printf '%s\\n' '" +
		`{"records":{"anthropic":{"refresh_token":"","access_token":"fixture-token","expiry":"2099-01-01T00:00:00Z"}}}` +
		"'; exit 0;;\n  *) exit 0;;\nesac\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "security"), []byte(security), 0700); err != nil {
		t.Fatal(err)
	}
	// The T627 sidecar withholding nothing: adopt is refused, load is ready.
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
	const seat = "po-fixture"
	rec := `{"ts":"2026-09-28T00:00:00Z","seat":"` + seat + `","type":"text","text":"an earlier turn"}` + "\n"
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
	body := fmt.Sprintf("owner_name: Test\noverseer_name: absent-overseer\nstate_dir: %q\nworkdir: %q\nprovider: claude\nmcp_server_name: reattach-fixture\nfrontier_consume:\n  disabled: true\n", state, root)
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	// A work seat, not the overseer: the overseer has its own recovery, and
	// the regression was the POs.
	defs, _ := json.Marshal([]claudia.AgentDef{{
		Name: seat, SessionID: "saved-po-fixture", WorkDir: root,
		Provider: claudia.Provider("anthropic"), Materialized: true, AutoStart: true,
		Purpose: claudia.PurposeWork,
	}})
	if err := os.WriteFile(filepath.Join(state, "agents.json"), defs, 0600); err != nil {
		t.Fatal(err)
	}
	brokerSock := filepath.Join(sockDir, "b.sock")
	env := []string{
		"HOME=" + home,
		"PATH=" + fakeBin + ":/usr/bin:/bin",
		"XDG_STATE_HOME=" + xdg,
		"CLAUDIA_BROKER_SOCKET=" + brokerSock,
		"CLAUDIA_OMP_SOCKET=" + filepath.Join(sockDir, "o.sock"),
		"CLAUDIA_OMP_SERVER=" + server,
		"STARTUP_MARKER=" + filepath.Join(root, "startup.json"),
		"FAKE_SIDECAR_WITHHOLD=none",
	}
	start := func(logName, name string, args ...string) (*exec.Cmd, chan error) {
		t.Helper()
		logs, err := os.OpenFile(filepath.Join(root, logName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { logs.Close() })
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = root, env, logs, logs
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		exited := make(chan struct{})
		go func() { done <- cmd.Wait(); close(exited) }()
		t.Cleanup(func() {
			// The whole group: whatever it started and did not detach.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-exited
		})
		return cmd, done
	}
	waitBroker := func() {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if c, err := net.Dial("unix", brokerSock); err == nil {
				c.Close()
				return
			}
			if time.Now().After(deadline) {
				b, _ := os.ReadFile(filepath.Join(root, "broker.log"))
				t.Fatalf("broker did not start: %s", b)
			}
		}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	status := func() string {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/agents", port))
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		var rows []struct {
			Name, Status string
		}
		if json.NewDecoder(resp.Body).Decode(&rows) != nil {
			return ""
		}
		for _, r := range rows {
			if r.Name == seat {
				return r.Status
			}
		}
		return ""
	}
	waitStatus := func(want string, within time.Duration, phase string) {
		t.Helper()
		deadline := time.Now().Add(within)
		for status() != want {
			if time.Now().After(deadline) {
				b, _ := os.ReadFile(filepath.Join(root, "jevonsd.log"))
				t.Fatalf("%s: %s is %q, want %q within %s:\n%s", phase, seat, status(), want, within, b)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	broker, brokerDone := start("broker.log", brokerBin, "broker", "serve")
	waitBroker()
	start("jevonsd.log", bin, "-config", cfg, "-port", fmt.Sprint(port), "-bind", "127.0.0.1", "-workdir", root)
	waitStatus("running", 30*time.Second, "boot")

	// Restart only the broker. It relaunches the seat on its own startup.
	if err := broker.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-brokerDone:
	case <-time.After(15 * time.Second):
		t.Fatal("broker did not stop")
	}
	start("broker.log", brokerBin, "broker", "serve")
	waitBroker()
	// One reconcile interval, plus the adopt itself.
	waitStatus("running", 45*time.Second, "after broker restart")
}
