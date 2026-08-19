// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// TestT526JevonsdRefusesJourneyPortOnDailyState is acceptance (1):
// `jevonsd -port 13715` with daily state_dir must refuse to boot (🎯T526).
func TestT526JevonsdRefusesJourneyPortOnDailyState(t *testing.T) {
	bin := buildJevonsdT526(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	daily := config.Default().StateDir
	body := fmt.Sprintf("state_dir: %q\nport: 13705\noverseer_name: jevons\n", daily)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "-config", cfgPath, "-port", "13715", "-workdir", t.TempDir())
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected refuse on journey port + daily state; output:\n%s", out)
	}
	got := string(out)
	if !strings.Contains(got, "journey port isolation") && !strings.Contains(got, "refusing port 13715") {
		t.Fatalf("refuse message missing; output:\n%s", got)
	}
}

// TestT526JevonsdAllowsJourneyPortOnIsolateState is the control: throwaway
// state_dir on the journey port gets past the T526 gate.
func TestT526JevonsdAllowsJourneyPortOnIsolateState(t *testing.T) {
	if err := config.RefuseJourneyDailyState(config.JourneyPort, t.TempDir()); err != nil {
		t.Fatalf("pure control must allow isolate state: %v", err)
	}

	bin := buildJevonsdT526(t)
	state := t.TempDir()
	cfgPath := filepath.Join(state, "config.yaml")
	body := fmt.Sprintf("state_dir: %q\nport: 13715\noverseer_name: jevons\nmcp_server_name: jevonsmcp-journey\n", state)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if out, err := exec.Command("lsof", "-nP", "-iTCP:13715", "-sTCP:LISTEN", "-t").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Skip("port 13715 held; pure RefuseJourneyDailyState control already passed")
	}

	cmd := exec.Command(bin, "-config", cfgPath, "-port", "13715", "-bind", "127.0.0.1", "-workdir", state)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		t.Fatalf("isolate boot exited before we stopped it (T526 refuse?): %v", err)
	case <-time.After(1500 * time.Millisecond):
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
}

func buildJevonsdT526(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "jevonsd")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build jevonsd: %v\n%s", err, out)
	}
	return bin
}
