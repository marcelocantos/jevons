// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

// 🎯T794: sticky OS notifier setup is not on the path to cockpit converge start.
func TestT794ConvergeBeforeStickyNotifier(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "🎯T794") {
		t.Fatal("main.go lost 🎯T794 boot-order comment")
	}
}
