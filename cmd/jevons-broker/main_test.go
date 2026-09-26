//go:build sibling_claudia

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

func TestBrokerIsASeparateProcess(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "package main") {
		t.Fatal("jevons-broker must be its own process")
	}
	if !strings.Contains(body, "daemon.New") {
		t.Fatal("jevons-broker must run the seat daemon")
	}
	if !strings.Contains(body, "EnsureOMPSidecar") {
		t.Fatal("jevons-broker must leave the sidecar ready")
	}
	if strings.Contains(body, "StopSidecar") {
		t.Fatal("a jevonsd/broker bounce must not kill the sidecar")
	}
	if !strings.Contains(body, "SetOMPToolExec") {
		t.Fatal("jevons-broker must attach jevons_* to the sidecar")
	}
	if !strings.Contains(body, "RemintRegistry") {
		t.Fatal("jevons-broker must remint the fleet onto the sidecar")
	}
	if !strings.Contains(body, "RefreshOMPPlans") {
		t.Fatal("jevons-broker must renew plan logins through pi-ai")
	}
	if !strings.Contains(body, "WithTimeout") || !strings.Contains(body, "RefreshOMPPlans(refreshCtx)") {
		t.Fatal("serve must bound plan refresh so a Keychain ACL prompt cannot hang the broker")
	}
	if !strings.Contains(body, "LoginOMPPlans") {
		t.Fatal("jevons-broker must create missing plan logins through pi-ai")
	}
	if !strings.Contains(body, `case "remint"`) {
		t.Fatal("jevons-broker must remint the fleet without serving")
	}
	if !strings.Contains(body, `case "smoke"`) {
		t.Fatal("jevons-broker must smoke Launch/Send/steer/abort on the sidecar")
	}
	if strings.Contains(body, `filepath.Join(*stateDir, "broker.sock")`) {
		t.Fatal("jevons-broker must use the claudia socket default, not ~/.jevons/broker.sock")
	}
	if !strings.Contains(body, "sockown.Claim") {
		t.Fatal("jevons-broker must claim the socket before Listen")
	}
	if !strings.Contains(body, `case "status"`) {
		t.Fatal("jevons-broker must report whether it owns the socket")
	}
}

func TestUsageWithoutServe(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
}
