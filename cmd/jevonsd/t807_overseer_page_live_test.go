// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/testreap"
)

const t807ThrowawayOverseer = "t807-throwaway-overseer"

// 🎯T807 live isolate: the production paging loop pages the owner (blurter
// sink) when the throwaway overseer is down, without waiting five minutes and
// without touching the development :13705 overseer.
func TestT807LiveIsolatePagesDeadThrowawayOverseer(t *testing.T) {
	testreap.Arm(t)
	bin := buildJevonsdT526(t)

	root := t.TempDir()
	state := filepath.Join(root, "state")
	home := filepath.Join(root, "home")
	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{state, home, fakeBin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	blurterLog := filepath.Join(root, "blurter.log")
	shim := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >>%q\necho /spool/accepted\n", blurterLog)
	if err := os.WriteFile(filepath.Join(fakeBin, "blurter"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, code := range map[string]string{"ps": "0", "launchctl": "0", "lsof": "1"} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte("#!/bin/sh\nexit "+code+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfgPath := filepath.Join(state, "config.yaml")
	const overseer = t807ThrowawayOverseer
	body := fmt.Sprintf(`owner_name: Test
overseer_name: %s
state_dir: %q
workdir: %q
provider: grok
mcp_server_name: t807-page-fixture
frontier_consume:
  disabled: true
`, overseer, state, root)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(root, "jevonsd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin, "-config", cfgPath, "-port", fmt.Sprint(port), "-bind", "127.0.0.1", "-workdir", root)
	cmd.Dir = root
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + fakeBin + ":/usr/bin:/bin",
		"JEVONS_TEST_FAULTS=1",
		"JEVONS_TEST_OVERSEER_PAGE_AFTER=600ms",
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	})

	// 8s missed the page under full-suite load; the loop returns on first sight.
	deadline := time.Now().Add(30 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(blurterLog)
		if err == nil && strings.Contains(string(b), "jevons-overseer-down") {
			got = string(b)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got == "" {
		daemonLog, _ := os.ReadFile(logPath)
		t.Fatalf("blurter never recorded overseer page within 30s; blurter log missing; daemon:\n%s", daemonLog)
	}
	if strings.Count(got, "jevons-overseer-down") != 1 {
		t.Fatalf("want one page per outage, blurter log:\n%s", got)
	}
	if !strings.Contains(got, "overseer") {
		t.Fatalf("page body should name overseer outage, got:\n%s", got)
	}
}
