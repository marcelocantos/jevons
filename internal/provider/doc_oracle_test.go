// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Doc completeness ratchet for 🎯T27.1 acceptance [doc].
// Asserts design/provider-contract.md names every required protocol surface.
func TestProviderContractDocCompleteness(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/provider → repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(root, "docs", "design", "provider-contract.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := string(body)
	required := []string{
		"manifest",
		"describe",
		`"feeds"`,
		`"ui"`,
		`"mcp"`,
		"ViewNode",
		"ViewProps",
		"launch",
		"connect",
		"/ws/provider",
		"endpoint",
		"origin",
		"loop-safety",
		"egress",
		"mock_ping",
		"internal/provider",
		"ios/Jevon/Models/ViewNode.swift",
		// 🎯T27.2 surfaces documented in the contract
		"config.yaml",
		"providers:",
		"registry.db",
		"ConfigManager",
	}
	for _, needle := range required {
		if !strings.Contains(doc, needle) {
			t.Errorf("docs/design/provider-contract.md missing required token %q", needle)
		}
	}
}
