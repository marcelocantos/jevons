// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestT541_3ComposeStartBriefDoesNotHoldMuAcrossRoleDisplay(t *testing.T) {
	s, reg := t541Server(t)
	const name = "jv-t541.3"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "sid-t541.3",
		Provider: claudia.ProviderCursor, Purpose: claudia.PurposeWork,
		Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = s.composeStartBrief(name, "Execute 🎯T541.3.")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("composeStartBrief held s.mu across roleDisplay/agentRole (🎯T541.3)")
	}
}
