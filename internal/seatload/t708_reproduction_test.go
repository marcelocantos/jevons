// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package seatload

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// The 🎯T708 reproduction, against real processes rather than a table.
//
// A fixture seat detaches an unbounded `while :; do sleep 1; done &` and
// then exits its own root — the 2026-09-20 shape, where the seat went idle
// and the loops kept running with their parent gone. The seat is then
// reaped the way the T165 path reaps one, and no descendant may survive.
//
// The control that makes this test worth having: the assertion runs after
// the root is dead, so a fix that only walks parent links passes nothing.
func TestT708ReapedSeatLeavesNoDescendant(t *testing.T) {
	// A shell that detaches an unbounded loop and then waits. Setpgid puts
	// it at the head of its own group, which is what the daemon anchors.
	cmd := exec.Command("/bin/sh", "-c", "while :; do sleep 1; done & sleep 120")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fixture seat: %v", err)
	}
	root := cmd.Process.Pid
	pgid := root
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}()

	tr := &Tracker{Grace: 300 * time.Millisecond}

	// Anchor while the seat is alive — the daemon records this at mint.
	var anchor Anchor
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err := tr.Track("jv-fixture-seat", root)
		if err == nil && a.PGID > 1 {
			tbl, terr := HostTable()
			if terr != nil {
				t.Fatalf("HostTable: %v", terr)
			}
			if len(Descendants(tbl, a)) > 0 {
				anchor = a
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture seat never showed a descendant (anchor %+v, err %v)", a, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The seat's own root goes away, exactly as an idled or reaped seat's
	// does. The loop is reparented to init and keeps the group.
	_ = syscall.Kill(root, syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
	waitGone(t, root)

	tbl, err := HostTable()
	if err != nil {
		t.Fatalf("HostTable: %v", err)
	}
	if got := len(Descendants(tbl, anchor)); got == 0 {
		t.Fatalf("no descendants survived the root's death — the reproduction did not reproduce")
	}

	res, err := tr.Reap("jv-fixture-seat")
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(res.Survived) != 0 {
		t.Fatalf("%s", res)
	}
	if len(res.Terminated) == 0 {
		t.Fatalf("reap terminated nothing: %s", res)
	}

	// The assertion the target asks for: no descendant survives.
	deadline = time.Now().Add(5 * time.Second)
	for {
		tbl, err := HostTable()
		if err != nil {
			t.Fatalf("HostTable: %v", err)
		}
		left := Descendants(tbl, anchor)
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d descendant(s) survived the reap: %+v", len(left), left)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d never exited", pid)
}
