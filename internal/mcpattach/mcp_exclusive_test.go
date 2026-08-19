// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestStampExclusiveFlipsAndFlagsGrokConnect(t *testing.T) {
	def := claudia.AgentDef{
		Name: "jevons", Provider: claudia.ProviderGrok,
		ConnectURL: "ws://127.0.0.1:9/ws", ConnectPID: 42,
	}
	if !StampExclusive(&def) {
		t.Fatal("want dropGrokConnect on first stamp")
	}
	if !def.MCPExclusive {
		t.Fatal("MCPExclusive not set")
	}
	if StampExclusive(&def) {
		t.Fatal("second stamp must be idempotent")
	}
}

func TestStampExclusiveClaudeDoesNotDropConnect(t *testing.T) {
	def := claudia.AgentDef{Name: "po", Provider: claudia.ProviderClaude, ConnectPID: 1}
	if StampExclusive(&def) {
		t.Fatal("Claude leftover is not a Grok serve")
	}
	if !def.MCPExclusive {
		t.Fatal("still stamp Exclusive")
	}
}
