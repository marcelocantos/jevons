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
	if !strings.Contains(body, "omp.Ensure") {
		t.Fatal("jevons-broker must leave the sidecar ready")
	}
	if strings.Contains(body, "StopSidecar") {
		t.Fatal("a jevonsd/broker bounce must not kill the sidecar")
	}
}

func TestUsageWithoutServe(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
}
