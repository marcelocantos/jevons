// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateKickoff(t *testing.T) {
	// Engaged → refuse.
	d := GateKickoff("identified", []string{"jv-t221-inspect-user-md"}, false)
	if d.Allow || d.Reason != "already_engaged" {
		t.Fatalf("engaged: %+v", d)
	}
	if !strings.Contains(d.Message, "jv-t221-inspect-user-md") {
		t.Fatalf("message=%q", d.Message)
	}

	// set_aside / achieved.
	if d := GateKickoff("set_aside", nil, false); d.Allow || d.Reason != "set_aside" {
		t.Fatalf("set_aside: %+v", d)
	}
	if d := GateKickoff("achieved", nil, false); d.Allow || d.Reason != "achieved" {
		t.Fatalf("achieved: %+v", d)
	}

	// Free open target.
	if d := GateKickoff("identified", nil, false); !d.Allow {
		t.Fatalf("free: %+v", d)
	}

	// Force overrides engagement.
	if d := GateKickoff("identified", []string{"w"}, true); !d.Allow {
		t.Fatalf("force: %+v", d)
	}
}

func TestLookupTargetStatus(t *testing.T) {
	yaml := []byte(`
targets:
  T220:
    name: X
    status: set_aside
`)
	st, ok := LookupTargetStatus(yaml, "🎯T220")
	if !ok || st != "set_aside" {
		t.Fatalf("st=%q ok=%v", st, ok)
	}
}

func TestLoadTargetStatusFromCwd(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`
targets:
  T1:
    name: One leaf
    status: identified
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	st, ok := LoadTargetStatusFromCwd(dir, "T1")
	if !ok || st != "identified" {
		t.Fatalf("st=%q ok=%v", st, ok)
	}
	if _, ok := LoadTargetStatusFromCwd(dir, "T999"); ok {
		t.Fatal("missing id must not ok")
	}
}

func TestIsOpenAndClosedStatus(t *testing.T) {
	if !IsOpenStatus("identified") || !IsOpenStatus("converging") || !IsOpenStatus("") {
		t.Fatal("open")
	}
	if IsOpenStatus("achieved") || IsOpenStatus("set_aside") {
		t.Fatal("not open")
	}
	if !IsClosedStatus("achieved") || !IsClosedStatus("set_aside") || !IsClosedStatus("set-aside") {
		t.Fatal("closed")
	}
	if IsClosedStatus("identified") {
		t.Fatal("not closed")
	}
}
