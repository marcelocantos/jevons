// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1095: a watchdog the owner disabled in launchd is an owner hold, not
// a gap. On 2026-10-20 the development daemon rewrote the plist and tried
// `launchctl bootstrap` every five minutes against a job listed disabled
// by `launchctl print-disabled gui/501`, failing each time with
// "Bootstrap failed: 5: Input/output error" — 58 attempts, each an ERROR.
package supervise_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// heldLaunchd is a gui domain whose watchdog the owner can switch off and
// on, recording every command it is asked to run.
type heldLaunchd struct {
	mu       sync.Mutex
	disabled bool
	loaded   bool
	calls    []string
}

func (l *heldLaunchd) run(args ...string) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, strings.Join(args, " "))
	switch args[0] {
	case "print-disabled":
		state := "enabled"
		if l.disabled {
			state = "disabled"
		}
		return []byte("disabled services = {\n\t\"homebrew.mxcl.jevons\" => disabled\n\t\"" +
			supervise.AgentLabel + "\" => " + state + "\n}\n"), nil
	case "print":
		if strings.HasSuffix(args[1], "/"+supervise.AgentLabel) && l.loaded {
			return []byte("state = running"), nil
		}
		return []byte("Could not find service"), errors.New("exit status 113")
	case "bootstrap":
		if l.disabled {
			return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
		}
		l.loaded = true
		return nil, nil
	}
	return []byte("unexpected"), errors.New("exit status 1")
}

func (l *heldLaunchd) set(disabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.disabled = disabled
}

func (l *heldLaunchd) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// TestT1095DisabledWatchdogIsAnOwnerHold drives the daemon's loop against
// a domain where the watchdog is disabled: no plist write, no bootstrap,
// one notice naming how to re-enable. Re-enabling resumes supervision on
// the next pass, and the recovery is reported.
func TestT1095DisabledWatchdogIsAnOwnerHold(t *testing.T) {
	ld := &heldLaunchd{disabled: true}
	supervise.FakeLaunchctl(t, ld.run)

	stateDir, home, repo := t.TempDir(), t.TempDir(), t.TempDir()
	dir := supervise.Dir(stateDir)
	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "bin", "jevons-watchdog"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Today's state on the owner's machine: a gap already reported and
	// 58 failed reinstatements behind it.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := supervise.SaveAgentState(dir, supervise.AgentState{Attempts: 58, Notified: true, LastAttempt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var notices []string
	notify := func(subject, kind, text string) bool {
		mu.Lock()
		defer mu.Unlock()
		notices = append(notices, text)
		return true
	}
	noticeCount := func() int { mu.Lock(); defer mu.Unlock(); return len(notices) }

	p := supervise.AgentPaths{StateDir: stateDir, Home: home, Repo: repo}
	cfg := supervise.AgentConfig{Stale: time.Hour, Retry: time.Millisecond}

	// Several passes over the held job: deterministic, driven directly
	// rather than by waiting on a real ticker (🎯T1102), so the test
	// cannot miss a pass or a notice because the host was busy.
	for i := 0; i < 6; i++ {
		supervise.CheckAgentOnce(p, cfg, notify)
	}
	if n := ld.count("print-disabled"); n < 6 {
		t.Fatalf("want at least 6 passes over the held job, got %d: %v", n, ld.calls)
	}
	if n := ld.count("bootstrap"); n != 0 {
		t.Fatalf("a disabled watchdog was bootstrapped %d time(s): %v", n, ld.calls)
	}
	plist := supervise.AgentPlistPath(home)
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatalf("the plist of a disabled watchdog was rewritten (stat err %v)", err)
	}
	mu.Lock()
	if len(notices) != 1 {
		mu.Unlock()
		t.Fatalf("want exactly one owner notice for the hold, got %d: %q", len(notices), notices)
	}
	hold := notices[0]
	mu.Unlock()
	if !strings.Contains(hold, "launchctl enable "+supervise.AgentDomain()+"/"+supervise.AgentLabel) {
		t.Fatalf("the hold notice does not say how to re-enable: %q", hold)
	}
	st, err := supervise.LoadAgentState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Held || st.Attempts != 0 {
		t.Fatalf("hold not recorded, or retry pacing kept: %+v", st)
	}

	// The owner re-enables it: the next pass reinstates.
	ld.set(false)
	supervise.CheckAgentOnce(p, cfg, notify)
	if n := ld.count("bootstrap"); n < 1 {
		t.Fatalf("want a bootstrap after re-enable, got %d: %v", n, ld.calls)
	}
	if _, err := os.Stat(plist); err != nil {
		t.Fatalf("re-enabled watchdog was bootstrapped without its plist: %v", err)
	}
	// It probes: the owner hears it close, and nothing else.
	if err := os.WriteFile(supervise.AgentLogPath(stateDir), []byte("probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	supervise.CheckAgentOnce(p, cfg, notify)
	if n := noticeCount(); n < 2 {
		t.Fatalf("want the recovery notice, got %d: %v", n, notices)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notices) != 2 || !strings.Contains(notices[1], "watchdog restored") {
		t.Fatalf("want the hold notice then one recovery notice, got %q", notices)
	}
}

// TestT1095EnabledButAbsentStillReinstates: the hold reads the override,
// not the absence. A job with no override is a gap, reinstated as before.
func TestT1095EnabledButAbsentStillReinstates(t *testing.T) {
	now := time.Now()
	obs := supervise.AgentObservation{Loaded: false, Binary: "/repo/bin/jevons-watchdog"}
	d, st := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, now)
	if !d.Reinstate || d.Held || d.Notify != supervise.NotifyProblem || st.Held {
		t.Fatalf("an enabled absent watchdog was not reinstated as a gap: %+v %+v", d, st)
	}

	obs.Disabled = true
	d, st = supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, now)
	if d.Reinstate || !d.Held || d.Notify != supervise.NotifyProblem || !st.Held || !st.Notified {
		t.Fatalf("a disabled watchdog was not held with one notice: %+v %+v", d, st)
	}
	d, _ = supervise.ClassifyAgent(agentCfg(), st, obs, now)
	if d.Reinstate || d.Notify != supervise.NotifyNone {
		t.Fatalf("a standing hold acted or re-notified: %+v", d)
	}
}

// TestT1095ReadsBothOverrideSpellings: macOS has printed overrides as
// `=> disabled` and, on older releases, `=> true`. Another label's
// override is not this job's.
func TestT1095ReadsBothOverrideSpellings(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want bool
	}{
		{"disabled services = {\n\t\"" + supervise.AgentLabel + "\" => disabled\n}\n", true},
		{"disabled services = {\n\t\"" + supervise.AgentLabel + "\" => true\n}\n", true},
		{"disabled services = {\n\t\"" + supervise.AgentLabel + "\" => enabled\n}\n", false},
		{"disabled services = {\n\t\"" + supervise.AgentLabel + "-x\" => disabled\n}\n", false},
		{"disabled services = {\n}\n", false},
	} {
		out := tc.out
		supervise.FakeLaunchctl(t, func(args ...string) ([]byte, error) { return []byte(out), nil })
		got, err := supervise.AgentDisabled(supervise.AgentLabel)
		if err != nil || got != tc.want {
			t.Fatalf("AgentDisabled(%q) = %v, %v; want %v", out, got, err, tc.want)
		}
	}
}
