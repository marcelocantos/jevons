// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT8667HomesLiveInJevons(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, rel := range []string{HomeRegistry, HomeBroker, HomeToolBodies, HomeConversationLog} {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || !info.IsDir() {
			t.Fatalf("T866.7 home %s missing: %v", rel, err)
		}
	}
	broker, err := os.ReadFile(filepath.Join(root, HomeBroker, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(broker)
	if !strings.Contains(body, "daemon.New") {
		t.Fatal("seat broker must remain a separate process")
	}
	if strings.Contains(body, "StopSidecar") {
		t.Fatal("a jevonsd bounce must not kill the sidecar")
	}
}
