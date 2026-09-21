// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newQueueServer(t *testing.T, path string, sender func(string) error) *Server {
	t.Helper()
	s := &Server{}
	s.notifyRetryDelay = time.Hour
	s.notifySender = sender
	if err := s.LoadOwnerQueue(path); err != nil {
		t.Fatalf("LoadOwnerQueue: %v", err)
	}
	return s
}

// 🎯T806: a refused owner message survives a daemon restart, is delivered
// after it, and a second restart cannot deliver it again.
func TestT806OwnerQueueSurvivesRestartAndDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner_queue.json")
	refuse := func(string) error { return fmt.Errorf(`broker protocol: not_owner (name="jevons")`) }

	s1 := newQueueServer(t, path, refuse)
	s1.registerOwnerMessageID(userTurnPrefix+"decisions", "om-1")
	_ = s1.SendToOverseer(userTurnPrefix + "decisions")
	if !queueHasOwner(s1.notifyQueue) {
		t.Fatal("refused message left the queue")
	}

	var got []string
	s2 := newQueueServer(t, path, func(text string) error { got = append(got, text); return nil })
	if !queueHasOwner(s2.notifyQueue) {
		t.Fatal("restart lost the refused owner message")
	}
	s2.drainOverseerNotes()
	if len(got) != 1 || !strings.Contains(got[0], "decisions") {
		t.Fatalf("want one delivery after restart, got %v", got)
	}

	var again []string
	s3 := newQueueServer(t, path, func(text string) error { again = append(again, text); return nil })
	s3.drainOverseerNotes()
	if len(again) != 0 || queueHasOwner(s3.notifyQueue) {
		t.Fatalf("restart re-delivered an already delivered message: %v", again)
	}
}

// A crash between delivery and record removal must not double-deliver: the
// delivered id is what replay dedupes on.
func TestT806ReplaySkipsDeliveredID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner_queue.json")
	data, _ := json.Marshal(ownerQueueFile{
		Pending:   []ownerQueueRecord{{ID: "om-7", Text: userTurnPrefix + "x"}},
		Delivered: []string{"om-7"},
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newQueueServer(t, path, func(string) error { return nil })
	if queueHasOwner(s.notifyQueue) {
		t.Fatal("replayed a message whose id is already delivered")
	}
}

// Malformed state is a hard error, never a silent reset.
func TestT806MalformedOwnerQueueIsHardError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner_queue.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Server{}).LoadOwnerQueue(path); err == nil {
		t.Fatal("malformed owner queue accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatal("malformed state was overwritten")
	}
}
