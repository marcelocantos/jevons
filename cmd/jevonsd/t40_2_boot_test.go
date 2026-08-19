// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

func TestBootAlwaysReattachFleet(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "upgrade.ReattachFleet(registry)") {
		t.Fatal("boot does not call ReattachFleet — crash/drain will Launch-only")
	}
	if strings.Contains(body, "registry.StartAll()") {
		t.Fatal("boot still has Launch-only StartAll — leftover+Launch is the ghost fleet")
	}
}

func TestContextCeilingSourceDoesNotRemint(t *testing.T) {
	src, err := os.ReadFile("ctxcap.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, bad := range []string{"PrepareCompaction", "compactFleetAgent", "CompactOverseer"} {
		if strings.Contains(body, bad) {
			t.Fatalf("ctxcap.go still remints via %s", bad)
		}
	}
	if !strings.Contains(body, "not reminting") {
		t.Fatal("ctxcap.go lost the observe-only log")
	}
	if !strings.Contains(body, "reporting unworkable") {
		t.Fatal("ctxcap.go lost the 🎯T417 unworkable report path")
	}
}
