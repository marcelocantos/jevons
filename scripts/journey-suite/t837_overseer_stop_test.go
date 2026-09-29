// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 🎯T837: an isolate start that ends "overseer not running yet: [{jevons
// stopped}]" names why the overseer is stopped, and is a harness outage.

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// The a20944c1 specimen (make test-journey ONLY=J31 PROVIDER=claude,
// 2026-09-22): boot took 42s of the 45s budget, the overseer launched and the
// daemon served for 3s before the readiness wait gave up. No cause logged.
const specimenSlowBoot = `time=2026-09-22T08:31:11.353+10:00 level=INFO msg="xAI API key loaded from Keychain"
time=2026-09-22T08:31:41.493+10:00 level=WARN msg="budget clamp-down" level=warn msg="budget: cost collector has not polled since never"
time=2026-09-22T08:31:41.499+10:00 level=INFO msg=notify_queue component=notify_queue decision=defer depth=1 deferred=1 err_class=not_running err="overseer not running" owner_batch=false
time=2026-09-22T08:31:49.830+10:00 level=INFO msg="jevon agent" provider=claude session=71326f40-0cf8-408e-a0c5-1206579ce4bc resume=false
time=2026-09-22T08:31:50.401+10:00 level=INFO msg="agent started" name=jevons provider=claude session=71326f40-0cf8-408e-a0c5-1206579ce4bc connect_pid=0 connect_url_set=false window=@1 materialized=false
time=2026-09-22T08:31:50.706+10:00 level=INFO msg="jevonsd starting" addr=127.0.0.1:50729 version=dev worker_model=""
`

// The 3d88ee5a specimen (make test-journey PROVIDER=grok ONLY=J3-,
// 2026-09-29): the Grok overseer's launch began and was still pending when
// the wait gave up; the only "auto-start failed" line is the suite's own
// interrupt cancelling that launch.
const specimenPendingLaunch = `time=2026-09-29T18:43:47.408+10:00 level=INFO msg="jevon agent" provider=grok session=ddb8bfbc-ee3a-4300-842e-4bb25952911c resume=false
time=2026-09-29T18:43:47.419+10:00 level=INFO msg="claudia daemon present; fleet seats are reclaimed, not reaped"
time=2026-09-29T18:44:23.409+10:00 level=INFO msg="shutting down" signal=interrupt exit_mode=normal stop_agents=false
time=2026-09-29T18:44:23.430+10:00 level=ERROR msg="auto-start failed" agent=jevons err="context canceled"
`

func TestT837StopReasonSlowBootIsStartTimeout(t *testing.T) {
	gaveUp := mustTime(t, "2026-09-22T08:31:53.600+10:00")
	got := overseerStopReason([]byte(specimenSlowBoot), "jevons", gaveUp, readyTimeout)
	for _, want := range []string{
		"start timeout after 45s",
		"overseer launched at 08:31:50.401, 3.2s before the deadline but had not reported running",
		"daemon began serving at 08:31:50.706, 2.9s before the deadline",
		"no launch error or plan-policy park",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reason missing %q:\n%s", want, got)
		}
	}
}

func TestT837StopReasonIgnoresTeardownCancel(t *testing.T) {
	gaveUp := mustTime(t, "2026-09-29T18:44:23.400+10:00")
	got := overseerStopReason([]byte(specimenPendingLaunch), "jevons", gaveUp, readyTimeout)
	if strings.Contains(got, "context canceled") {
		t.Fatalf("teardown's cancel reported as the cause:\n%s", got)
	}
	for _, want := range []string{
		"start timeout",
		"overseer launch (provider=grok resume=false) began at 18:43:47.408, 36s before the deadline and never finished",
		"daemon never logged that it began serving",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reason missing %q:\n%s", want, got)
		}
	}
}

func TestT837StopReasonNamesLoggedCauses(t *testing.T) {
	for _, tc := range []struct {
		name, log string
		want      []string
	}{
		{
			name: "launch error",
			log: `time=2026-09-22T09:50:01.000+10:00 level=INFO msg="jevon agent" provider=grok session=4b7806bc resume=true
time=2026-09-22T09:50:02.000+10:00 level=ERROR msg="auto-start failed" agent=jevons err="exclusive GROK_HOME unavailable for session 4b7806bc: stat /x/claudia/grok-homes/4b7806bc: no such file or directory"
time=2026-09-22T09:50:02.100+10:00 level=ERROR msg="OVERSEER NOT RUNNING — chat cannot respond until this is fixed" overseer=jevons provider=grok likely_cause="grok launch failed"
`,
			want: []string{
				"launch error at 09:50:02.000: exclusive GROK_HOME unavailable for session 4b7806bc",
				"daemon reported OVERSEER NOT RUNNING at 09:50:02.100 (provider=grok): grok launch failed",
			},
		},
		{
			name: "cockpit relaunch error",
			log:  `time=2026-09-22T09:50:02.000+10:00 level=WARN msg="cockpit: overseer launch failed" name=jevons attempt=2 err="claude held by broker"` + "\n",
			want: []string{"launch error at 09:50:02.000: claude held by broker"},
		},
		{
			name: "plan-policy park",
			log:  `time=2026-09-22T08:31:45.000+10:00 level=INFO msg="plan policy parked" name=jevons from=claude` + "\n",
			want: []string{"plan-policy park at 08:31:45.000: plan policy parked from=claude"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := overseerStopReason([]byte(tc.log), "jevons", time.Time{}, readyTimeout)
			if strings.Contains(got, "start timeout") {
				t.Errorf("logged cause reported as a timeout:\n%s", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("reason missing %q:\n%s", want, got)
				}
			}
		})
	}
}

// Control: another seat's failure is not the overseer's stop reason.
func TestT837StopReasonIgnoresOtherAgents(t *testing.T) {
	log := `time=2026-09-22T09:50:02.000+10:00 level=ERROR msg="auto-start failed" agent=jv-worker err="boom"
time=2026-09-22T09:50:03.000+10:00 level=INFO msg="plan policy parked" name=jv-worker from=claude
`
	got := overseerStopReason([]byte(log), "jevons", time.Time{}, readyTimeout)
	if strings.Contains(got, "boom") || strings.Contains(got, "park at") {
		t.Fatalf("another agent's failure attributed to the overseer:\n%s", got)
	}
	if !strings.Contains(got, "overseer launch never began") {
		t.Fatalf("want start timeout with no launch:\n%s", got)
	}
}

// fakeDaemonEnv selects the fake-daemon child mode in TestMain; its log
// lines come from fakeDaemonLogEnv.
const (
	fakeDaemonMode   = "t837-fake-daemon"
	fakeDaemonLogEnv = "T837_FAKE_DAEMON_LOG"
)

// runFakeStoppedDaemon stands in for jevonsd: it prints its boot log, serves
// /health and an /api/agents whose overseer is stopped, and on interrupt logs
// the teardown the real daemon does — including an "auto-start failed ...
// context canceled" that must not be read as the cause.
func runFakeStoppedDaemon() error {
	port := ""
	for i, a := range os.Args {
		if a == "-port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return err
	}
	fmt.Print(os.Getenv(fakeDaemonLogEnv))
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"jevons","status":"stopped"}]`)
	})
	go func() { _ = http.Serve(ln, mux) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	now := time.Now().Format(time.RFC3339Nano)
	fmt.Printf("time=%s level=INFO msg=\"shutting down\" signal=interrupt\n", now)
	fmt.Printf("time=%s level=ERROR msg=\"auto-start failed\" agent=jevons err=\"context canceled\"\n", now)
	return nil
}

func startFakeStopped(t *testing.T, bootLog, priorLog string) error {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jevonsd.log")
	if err := os.WriteFile(logPath, []byte(priorLog), 0o600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logFile.Close() })
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	s := &suite{
		host: fmt.Sprintf("127.0.0.1:%d", port), stateDir: dir, provider: "claude",
		daemonBin: os.Args[0], cfgPath: filepath.Join(dir, "config.yaml"), logPath: logPath,
		workdir: dir, port: port, logFile: logFile,
		daemonEnv: []string{childEnv + "=" + fakeDaemonMode, fakeDaemonLogEnv + "=" + bootLog},
		readyWait: 3 * time.Second,
	}
	t.Cleanup(func() { _ = s.signalStop(2 * time.Second) })
	return s.startDaemon()
}

// The readiness wait itself reports the stop reason, as an outage, and reads
// it before the teardown's own cancel lands in the log.
func TestT837ReadinessWaitReportsStopReason(t *testing.T) {
	boot := `time=2026-09-22T09:50:01.000+10:00 level=INFO msg="jevon agent" provider=grok session=4b7806bc resume=true
time=2026-09-22T09:50:02.000+10:00 level=ERROR msg="auto-start failed" agent=jevons err="exclusive GROK_HOME unavailable for session 4b7806bc"
`
	// A previous start in the same run failed differently; its bytes are
	// not this start's cause.
	prior := `time=2026-09-22T09:40:00.000+10:00 level=ERROR msg="auto-start failed" agent=jevons err="an earlier start's failure"` + "\n"
	err := startFakeStopped(t, boot, prior)
	if err == nil {
		t.Fatal("start succeeded with a stopped overseer")
	}
	if !isOutage(err) {
		t.Fatalf("stopped overseer is not classified as an outage: %v", err)
	}
	var notRunning *overseerNotRunningError
	if !errors.As(err, &notRunning) {
		t.Fatalf("outage lost the readiness verdict: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"overseer not running yet: [{jevons stopped}]",
		"launch error at 09:50:02.000: exclusive GROK_HOME unavailable for session 4b7806bc",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	for _, bad := range []string{"context canceled", "an earlier start's failure"} {
		if strings.Contains(msg, bad) {
			t.Errorf("error names %q, which is not this start's cause:\n%s", bad, msg)
		}
	}
}

// With nothing logged, the same wait says it ran out of time.
func TestT837ReadinessWaitReportsStartTimeout(t *testing.T) {
	err := startFakeStopped(t, "", "")
	if err == nil || !isOutage(err) {
		t.Fatalf("want an outage, got %v", err)
	}
	if !strings.Contains(err.Error(), "start timeout after 3s — overseer launch never began") {
		t.Fatalf("error does not name the start timeout:\n%v", err)
	}
}
