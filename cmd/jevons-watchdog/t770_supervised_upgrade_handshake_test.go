// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T770: restart-jevonsd must not report failure while supervisord is still
// completing a slow supervised upgrade exit. The 2026-09-21 incident died at
// STOP_WAIT_SEC even though the relaunch landed ~30s later.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func t770BuildSlowExitUpgradeStub(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "slowupgradestub")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	const prog = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 0, "")
	exitDelay := flag.Int("exit-delay", 0, "seconds after SIGHUP before exit")
	flag.Parse()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		<-hup
		if *exitDelay > 0 {
			time.Sleep(time.Duration(*exitDelay) * time.Second)
		}
		os.Exit(0)
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/api/frontier", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "[]") })
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module slowupgradestub\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "slowupgradestub-bin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the slow upgrade stub: %v\n%s", err, b)
	}
	return bin
}

func t770Restart(r *rig, s *t1086Supervisord, waitSec int) error {
	return r.restart(
		"JEVONS_RESTART_SUPERVISOR_CONF="+s.conf,
		fmt.Sprintf("JEVONS_RESTART_WAIT_SEC=%d", waitSec),
		"JEVONS_RESTART_STOP_WAIT_SEC=5",
		"JEVONS_RESTART_RELAUNCH_WAIT_SEC=5",
	)
}

func TestT770SlowSupervisedUpgradeExitCompletesBeforeHandshakeTimeout(t *testing.T) {
	r := newRig(t)
	stub := t770BuildSlowExitUpgradeStub(t, r.dir)
	// Exit 8s after SIGHUP. The old script failed at STOP_WAIT_SEC=5s with
	// "supervised daemon did not finish upgrade exit"; the handshake budgets
	// STOP_WAIT_SEC + RELAUNCH_WAIT_SEC (=10s here).
	s := t1086StartSupervisord(t, r, "true", fmt.Sprintf("%s -port %d -exit-delay 8", stub, r.port))
	before := s.programPID()

	if err := t770Restart(r, s, 30); err != nil {
		t.Fatalf("restart under slow supervised upgrade exit failed: %v", err)
	}
	if !r.serving() {
		t.Fatal("daemon down after the bounce")
	}
	if after := s.programPID(); after == 0 || after == before {
		t.Fatalf("supervisord program pid before=%d after=%d; want a new supervised process", before, after)
	}
	log := t405ReadFile(r.restartLog())
	if strings.Contains(log, "supervised daemon did not finish upgrade exit") {
		t.Fatalf("restart log still reports the 🎯T770 false failure:\n%s", t405Tail(log, 30))
	}
}
