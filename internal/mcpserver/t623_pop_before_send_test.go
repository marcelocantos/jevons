// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"
)

// This regression uses only pre-existing APIs so it also runs against the
// old drain as a negative control. Inspect at the provider boundary, not after
// the daemon has had an opportunity to repair its queue.
func TestT623AcceptedPayloadIsPresentDuringProviderSubmit(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	original, _, err := s.sendQueue().Append("a", "accepted before provider submission", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	proc := &chipSender{alive: true, onPaste: func(text string) {
		called = true
		entries, err := s.sendQueue().Snapshot("a")
		if err != nil || len(entries) != 1 || entries[0].ID != original.ID || entries[0].Text != text {
			t.Errorf("accepted payload vanished during provider submission: %+v %v", entries, err)
		}
	}}
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return proc, false, nil })
	s.drainAgentSendQueue("a")
	if !called {
		t.Fatal("the production drain never submitted")
	}
	if depth := s.pendingAgentSends("a"); depth != 0 {
		t.Fatalf("healthy delivery left %d held", depth)
	}
}
