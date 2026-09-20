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
	"strconv"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/portown"
)

func TestInspectPortOwnershipDoesNotTreatSquatterAsOutage(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "inspectPortOwnership") {
		t.Fatal("watchdog cycle must call inspectPortOwnership")
	}
	if !strings.Contains(body, "independent of Decide") {
		t.Fatal("squatter inspect must stay independent of Decide/ActionRestart")
	}
}

func TestPortConflictNoticeUsesDistinctBlurterKey(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("port.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "jevonsd-port-squatter-") {
		t.Fatal("port-squatter notices must not share the down-key")
	}
	if strings.Contains(body, "jevonsd-down-") {
		t.Fatal("port.go must not reuse the outage blurter key")
	}
}

func TestAlarmTextNamesPid(t *testing.T) {
	c := portown.Classify([]portown.Listener{
		{PID: 11, Cmd: "jevonsd", Addr: "127.0.0.1:9"},
		{PID: 22, Cmd: "jevond", Addr: "*:9"},
	})
	text := portown.AlarmText(c, portown.Fleet{
		Loopback:  portown.Probe{OK: true, Fingerprint: "a"},
		Localhost: portown.Probe{OK: true, Fingerprint: "a"},
	})
	if !strings.Contains(text, "22") {
		t.Fatalf("got %q", text)
	}
}

func TestInspectPortOwnershipBlurtsSquatterPid(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "blurter.log")
	shim := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >>%q\n", logPath)
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "jevons"}})
	})
	go http.Serve(ln, mux)

	stubPID, stop := startWatchdogWildcardStub(t, port)
	defer stop()

	inspectPortOwnership(port, true)
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("blurter log: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "jevonsd-port-squatter-"+strconv.Itoa(port)) {
		t.Fatalf("missing squatter key in %q", got)
	}
	if !strings.Contains(got, strconv.Itoa(stubPID)) {
		t.Fatalf("must name squatting pid %d in %q", stubPID, got)
	}

	inspectPortOwnership(port, false)
	b2, _ := os.ReadFile(logPath)
	if string(b2) != got {
		t.Fatal("serving=false must not send another notice (T405 owns the outage)")
	}
}

func startWatchdogWildcardStub(t *testing.T, port int) (pid int, stop func()) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required to hold a * listener in another pid")
	}
	script := fmt.Sprintf(`
import socket, time, os, sys
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", %d))
s.listen(1)
sys.stdout.write(str(os.getpid()) + "\n")
sys.stdout.flush()
time.sleep(60)
`, port)
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("wildcard stub: %v", err)
	}
	buf := make([]byte, 32)
	n, err := out.Read(buf)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Skipf("kernel refused * beside 127.0.0.1: %v", err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("stub pid: %v (%q)", err, buf[:n])
	}
	var stopped bool
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return pid, stop
}
