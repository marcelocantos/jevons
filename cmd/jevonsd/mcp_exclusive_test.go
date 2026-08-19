// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestStampRegistryMCPExclusiveOverseerAndWorkers(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", SessionID: "po", Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	overseer := &claudia.AgentDef{
		Name: "jevons", SessionID: "root", Provider: claudia.ProviderGrok,
		ConnectURL: "ws://127.0.0.1:9/ws", ConnectPID: 999999,
	}
	stampRegistryMCPExclusive(reg, overseer)
	if !overseer.MCPExclusive {
		t.Fatal("overseer Exclusive")
	}
	if overseer.ConnectURL != "" || overseer.ConnectPID != 0 {
		t.Fatalf("overseer leftover connect survived: %+v", overseer)
	}
	po := reg.Def("jevons-po")
	if po == nil || !po.MCPExclusive {
		t.Fatalf("PO Exclusive: %+v", po)
	}
}
