// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/delivery"
)

// A send to a name that was never registered records nothing. Recording first
// wrote the owner's words into that name's transcript as a delivered turn, and
// the composer read the echo as success and dropped the draft of a send that
// had failed (journey J36, 2026-10-05).
func TestSendToUnregisteredNameRecordsNothing(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	var recorded []string
	s.SetAgentRequestRecorder(func(name, _ string, _ SendOrigin) error {
		recorded = append(recorded, name)
		return nil
	})
	if _, err := s.DeliverAgentMessageMode("never-registered", "hello", OriginOwner, delivery.ModeSubmit); err == nil {
		t.Fatal("a send to a never-registered name succeeded")
	}
	if len(recorded) != 0 {
		t.Fatalf("recorded a turn for a never-registered name: %v", recorded)
	}
}
