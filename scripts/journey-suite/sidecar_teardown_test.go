// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia/omp"
)

const sidecarHolderMode = "sidecar-holder"

func runSidecarHolder(socket string) error {
	f, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	if err := os.WriteFile(socket+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return err
	}
	fmt.Println("ready")
	time.Sleep(time.Minute)
	return nil
}

// The J38 teardown failure of gate 252cf6ac: the broker had exited, its
// sidecar had followed it out (the claudia 🎯T166 lifeline), and the pid file
// still named the dead process. Signalling that pid reports "os: process
// already finished"; the lock says no sidecar is serving, which is the state
// teardown wants.
func TestStopIsolatedSidecarAfterSidecarAlreadyExited(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "omp.sock")
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socket+".pid", []byte(strconv.Itoa(dead.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	// The mechanism of the failure, pinned so the test says why it exists.
	if err := omp.StopSidecar(socket); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("omp.StopSidecar on a pid file naming an exited sidecar = %v, want os.ErrProcessDone", err)
	}
	if err := os.WriteFile(socket+".pid", []byte(strconv.Itoa(dead.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stopIsolatedSidecar(socket, time.Second); err != nil {
		t.Fatalf("teardown after the sidecar already exited: %v", err)
	}
}

// A sidecar that outlives its broker is still stopped.
func TestStopIsolatedSidecarStopsALiveHolder(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "omp.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"="+sidecarHolderMode, "JOURNEY_SIDECAR_SOCKET="+socket)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("holder did not start: %q %v", line, err)
	}
	if free, err := sidecarLockFree(socket); err != nil || free {
		t.Fatalf("lock free=%v err=%v while the holder runs", free, err)
	}
	if err := stopIsolatedSidecar(socket, 200*time.Millisecond); err != nil {
		t.Fatalf("stop a live sidecar: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the live sidecar was not stopped")
	}
}
