// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiptPayloadPreservesAdmissionIdentity(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "original report", "owner", time.Now())
	if err != nil || !claimed {
		t.Fatalf("admit: %v %v", claimed, err)
	}
	if _, err := q.BindReceiptPayload("a", e, "generation", "stamped report"); err != nil {
		t.Fatal(err)
	}
	for _, store := range []*Store{q, NewStore(dir)} {
		for _, text := range []string{"original report", "stamped report", "changed report"} {
			_, _, _, err := store.AppendAndClaimWithRequestID("a", text, "owner", time.Now())
			duplicate, ok := errors.AsType[*DuplicateRequestIDError](err)
			if !ok || duplicate.Conflict != (text != "original report") || duplicate.Existing.Text != "original report" {
				t.Fatalf("duplicate %q lost original admission identity: %v", text, err)
			}
		}
	}
}

func TestReceiptPayloadFailedWriteLeavesAdmissionUnchanged(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "original report", "owner", time.Now())
	if err != nil || !claimed {
		t.Fatalf("admit: %v %v", claimed, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "a.json.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := q.BindReceiptPayload("a", e, "generation", "stamped report"); err == nil {
		t.Fatal("failed binding write reported success")
	}
	rows, err := NewStore(dir).Snapshot("a")
	if err != nil || len(rows) != 1 || rows[0] != e {
		t.Fatalf("failed binding changed durable admission: %+v %v", rows, err)
	}
}

func TestReceiptPayloadFrozenBeforeWireAndAcrossRetry(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "original report", "owner", time.Now())
	if err != nil || !claimed {
		t.Fatalf("admit: %v %v", claimed, err)
	}
	e, err = q.BindReceiptPayload("a", e, "generation", "first stamped report")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := NewStore(dir).Snapshot("a")
	if err != nil || len(rows) != 1 || rows[0].Text != "original report" || rows[0].WirePayload() != "first stamped report" || rows[0].TransportRequestID != "owner" {
		t.Fatalf("wire payload not durable: %+v %v", rows, err)
	}
	if err := q.Resolve("a", e, DefinitelyNotSent, "pre-write refusal"); err != nil {
		t.Fatal(err)
	}
	q = NewStore(dir)
	e, claimed, err = q.ClaimFront("a")
	if err != nil || !claimed {
		t.Fatalf("reclaim: %v %v", claimed, err)
	}
	e, err = q.BindReceiptPayload("a", e, "generation", "newly stamped report")
	if err != nil || e.Text != "original report" || e.WirePayload() != "first stamped report" || e.TransportRequestID != "owner" {
		t.Fatalf("retry changed submitted payload: %+v %v", e, err)
	}
}

func TestReceiptPayloadEmptyAndLegacyBindings(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty-wire", true: "legacy-binding"}[legacy], func(t *testing.T) {
			dir := t.TempDir()
			q := NewStore(dir)
			e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "original report", "owner", time.Now())
			if err != nil || !claimed {
				t.Fatalf("admit: %v %v", claimed, err)
			}
			want := ""
			if legacy {
				e, err = q.BindReceipt("a", e, "generation")
				want = "original report"
			} else {
				e, err = q.BindReceiptPayload("a", e, "generation", "")
			}
			if err != nil {
				t.Fatal(err)
			}
			rows, err := NewStore(dir).Snapshot("a")
			if err != nil || len(rows) != 1 || rows[0].WirePayload() != want || rows[0].Text != "original report" {
				t.Fatalf("restored wire payload: %+v %v", rows, err)
			}
			// A previously bound request cannot acquire a newly decorated payload.
			e, err = q.BindReceiptPayload("a", e, "generation", "later decoration")
			if err != nil || e.WirePayload() != want {
				t.Fatalf("existing binding changed: %+v %v", e, err)
			}
		})
	}
}

func TestReceiptPayloadCorruptPresenceIsNotRewritten(t *testing.T) {
	for _, fields := range []string{
		`"wire_text":"unmarked"`,
		`"wire_text_set":true`,
		`"wire_text":"unmarked","session_id":"generation","transport_request_id":"sendq:entry"`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.json")
		body := []byte(`{"agent":"a","entries":[{"id":"entry","text":"original",` + fields + `}]}`)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(dir).Snapshot("a"); err == nil {
			t.Fatalf("corrupt wire payload accepted: %s", fields)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != string(body) {
			t.Fatalf("corrupt queue changed: %s %v", after, err)
		}
	}
}

func TestReceiptUrgentOvertakesPendingWithoutStealingActive(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	first, _, err := q.Append("a", "old pending", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	urgent, depth, claimed, err := q.AppendAndClaimUrgent("a", "urgent owner input", "owner", time.Now())
	if err != nil || !claimed || depth != 2 {
		t.Fatalf("urgent admission: claimed=%v depth=%d err=%v", claimed, depth, err)
	}
	if _, _, _, err := q.AppendAndClaimUrgent("a", "another urgent input", "second", time.Now()); !errors.Is(err, ErrActiveHandoff) {
		t.Fatalf("urgent admission stole active handoff: %v", err)
	}
	if errors.Is(fmt.Errorf("operator note mentions an active handoff"), ErrActiveHandoff) {
		t.Fatal("a mention of the words is the sentinel")
	}
	if _, claimed, err := q.ClaimFront("a"); err != nil || claimed {
		t.Fatalf("ordinary claim stole active urgent handoff: %v %v", claimed, err)
	}
	rows, err := NewStore(dir).Snapshot("a")
	if err != nil || len(rows) != 2 || rows[0].ID != first.ID || rows[1].ID != urgent.ID {
		t.Fatalf("refused urgency was silently enqueued: %+v %v", rows, err)
	}
	if err := q.Resolve("a", urgent, Unverified, "acknowledgement lost"); err != nil {
		t.Fatal(err)
	}
	next, claimed, err := q.ClaimFront("a")
	if err != nil || !claimed || next.ID != first.ID {
		t.Fatalf("urgent handoff reordered pending backlog: %+v %v %v", next, claimed, err)
	}
}
