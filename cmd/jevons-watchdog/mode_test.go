// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"github.com/marcelocantos/jevons/internal/supervise"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// No real launchd job or daemon is touched. A missing repo makes a mistaken
// legacy action visible without needing a restart script or a listening port.
func TestKeepAliveNeverCompetesWithLaunchd(t *testing.T) {
	repo := t.TempDir()
	script := filepath.Join(repo, "scripts", "restart-jevonsd.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho touched >"+filepath.Join(repo, "touched")+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	detail, legacy := restartForMode(repo, 12345, true, nil)
	if legacy {
		t.Fatal("KeepAlive mode claimed a legacy restart")
	}
	if !strings.Contains(detail, "KeepAlive is loaded") || !strings.Contains(detail, "no competing kickstart") {
		t.Fatalf("missing passive recovery explanation: %q", detail)
	}
	if _, err := os.Stat(filepath.Join(repo, "touched")); !os.IsNotExist(err) {
		t.Fatalf("restart script ran under KeepAlive: %v", err)
	}
}

func TestUnknownModeFailsClosedAndUnloadedModeFallsBack(t *testing.T) {
	repo := t.TempDir() // no script: the legacy route reports this exact absence
	if got, legacy := restartForMode(repo, 12345, false, errors.New("launchctl unavailable")); legacy || !strings.Contains(got, "refusing a competing restart") {
		t.Fatalf("unknown mode: %q", got)
	}
	if got, legacy := restartForMode(repo, 12345, false, nil); !legacy || !strings.Contains(got, "no restart script") {
		t.Fatalf("legacy fallback: %q", got)
	}
}

// A loaded→unloaded transition changes the next attempt's action without a
// watchdog restart or a daemon-start snapshot. The legacy route must really
// invoke the script, not merely say that the script exists.
func TestWatchdogSwitchesRecoveryAuthorityPerAttempt(t *testing.T) {
	repo := t.TempDir()
	script := filepath.Join(repo, "scripts", "restart-jevonsd.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(repo, "invoked")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'started' >"+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	if detail, legacy := restartForMode(repo, port, true, nil); legacy || !strings.Contains(detail, "KeepAlive is loaded") {
		t.Fatalf("loaded mode: %q legacy=%v", detail, legacy)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("script ran with KeepAlive loaded: %v", err)
	}
	if detail, legacy := restartForMode(repo, port, false, nil); !legacy || detail != "" {
		t.Fatalf("unloaded fallback: %q legacy=%v", detail, legacy)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("legacy script was not invoked: %v", err)
	}
}

func TestPassiveOutageStillAlarmsOutOfBand(t *testing.T) {
	dir := t.TempDir()
	blurter := filepath.Join(dir, "blurter")
	output := filepath.Join(dir, "notice")
	if err := os.WriteFile(blurter, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >"+output+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	now := time.Now()
	cfg := supervise.DefaultConfig()
	d, _ := supervise.Decide(cfg, supervise.State{DownSince: now.Add(-2 * cfg.Grace)}, false, now)
	if d.Notify != supervise.NotifyProblem {
		t.Fatalf("no outage alarm: %+v", d)
	}
	detail, legacy := restartForMode(dir, 12345, true, nil)
	if legacy {
		t.Fatal("passive mode tried legacy recovery")
	}
	notify(d, 12345, detail)
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("no out-of-band notice: %v", err)
	}
	for _, want := range []string{"--severity problem", "KeepAlive is loaded", "no competing kickstart"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("notice lacks %q: %s", want, b)
		}
	}
}
