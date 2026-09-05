// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T593. 🎯T548.2 made SQLite the durable transcript store and stopped
// taking the JSONL append path — but send-landed was only ever observed on
// that append, inside persistChatJSONL. So sendJournaled could never become
// true on a statedb deployment, every owner turn aged into the
// not_durable_in_chatlog gap, and owner-health re-injected it. On
// 2026-08-31 one owner question reached the overseer three times while the
// cockpit sat at idle.
func TestOwnerSendLandsOnAStatedbBackedServer(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dir := t.TempDir()
	s := New("test", dir)
	s.SetStateDB(db)

	// A configured chatlog is what makes sendJournaled start false
	// (NoteOwnerSend: sendJournaled = !hasChatLog()). Without one the bug
	// is unreachable, so the fixture must have it — this is the shape the
	// owner actually runs.
	clog, err := chatlog.Open(filepath.Join(dir, "chat.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetChatLog(clog)

	text := "why is jv-t557 idle?"
	echo := chatUserEcho(text)
	s.NoteOwnerSend(text, echo)

	if o := s.ObserveOwnerInteraction(time.Now()); o.SendJournaled {
		t.Fatal("fixture is wrong: durability must start unobserved, or the test proves nothing")
	}

	// First line seeds statedb; from here SQLite is the live store and the
	// JSONL branch is not taken again.
	s.persistChatLine(echo)
	if n := s.statedbN(s.overseerAgentName()); n == 0 {
		t.Fatal("fixture did not populate statedb; the JSONL path would still be carrying durability")
	}
	s.persistChatLine(echo)

	o := s.ObserveOwnerInteraction(time.Now())
	if !o.SendJournaled {
		t.Fatal("owner send never recorded as durable — owner-health re-injects it forever")
	}

	// The verdict the re-injection loop hung on must now be satisfied, and
	// checked past the land bound so "within_land_bound" cannot mask it.
	o.SendDelivered = true
	late := time.Now().Add(10 * time.Minute)
	for _, v := range converge.ClassifyOwnerInteraction(o, late, converge.DefaultOwnerBounds()) {
		if v.Dimension != converge.OwnerDimSendLanded {
			continue
		}
		if v.Reason == "not_durable_in_chatlog" {
			t.Fatalf("send still reads not_durable_in_chatlog: %+v", v)
		}
		if v.Condition != converge.ConditionSatisfied {
			t.Fatalf("send_landed unsatisfied: %+v", v)
		}
	}
}

// A failed durable write must not report success: the JSONL path only
// noted journaling after Append returned nil, and the statedb path has to
// hold the same line, or the fix trades a false gap for a false green —
// the worse of the two.
//
// The lower-level signal and the owner-facing caller must agree. A write to
// obsolete JSONL is not durable in the canonical conversation's recovery path.
func TestDurableWriteFailureIsReportedAsNotDurable(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := New("test", dir)
	s.SetStateDB(db)

	name := s.overseerAgentName()
	if !s.muxFanTranscript(name, chatUserEcho("while the store is open")) {
		t.Fatal("a healthy write must report durable, or the false case below proves nothing")
	}

	_ = db.Close()
	if s.muxFanTranscript(name, chatUserEcho("after the store is gone")) {
		t.Fatal("a failed durable write reported itself durable")
	}
}

func TestOwnerPersistenceFailureDoesNotClaimLegacyFallbackAsDurable(t *testing.T) {
	for _, shape := range []string{"empty", "populated", "sqlite-only"} {
		t.Run(shape, func(t *testing.T) {
			dir := t.TempDir()
			db, err := statedb.Open(filepath.Join(dir, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := New("test", dir)
			s.SetStateDB(db)
			clog, err := chatlog.Open(filepath.Join(dir, "chat.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer clog.Close()
			if shape != "sqlite-only" {
				s.SetChatLog(clog)
			}
			s.ImportTranscripts()
			if shape == "populated" {
				s.persistChatLine(chatUserEcho("earlier request"))
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			text := "retain this request for recovery"
			echo := chatUserEcho(text)
			s.NoteOwnerSend(text, echo)
			s.persistChatLine(echo)
			if s.ObserveOwnerInteraction(time.Now()).SendJournaled {
				t.Fatal("failed canonical write was reported durable via obsolete JSONL")
			}
			if n := statedb.JSONLSize(clog.Path()); n != 0 {
				t.Fatalf("failed canonical write silently switched stores: JSONL bytes=%d", n)
			}
		})
	}
}
