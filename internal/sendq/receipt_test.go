// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReceiptBindingSurvivesRestartAndRejectsRebinding(t *testing.T) {
	for _, ownerID := range []string{"", "owner-question"} {
		t.Run("owner="+ownerID, func(t *testing.T) {
			dir := t.TempDir()
			q := NewStore(dir)
			e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "payload", ownerID, time.Now())
			if err != nil || !claimed {
				t.Fatalf("admit: %+v claimed=%v err=%v", e, claimed, err)
			}
			bound, err := q.BindReceipt("a", e, "generation-one")
			if err != nil {
				t.Fatal(err)
			}
			wantID := ownerID
			if wantID == "" {
				wantID = "sendq:" + e.ID
			}
			if bound.RequestID != ownerID || bound.TransportRequestID != wantID || bound.SessionID != "generation-one" || bound.AttemptID != e.AttemptID {
				t.Fatalf("binding changed identity: %+v", bound)
			}
			// Idempotent within the owning attempt; no new transport identity.
			if again, err := q.BindReceipt("a", e, "generation-one"); err != nil || again != bound {
				t.Fatalf("same binding: %+v %v", again, err)
			}
			if _, err := q.BindReceipt("a", bound, "generation-two"); err == nil {
				t.Fatal("rebound current attempt to another generation")
			}
			after := NewStore(dir)
			entries, err := after.Snapshot("a")
			if err != nil || len(entries) != 1 || entries[0] != bound {
				t.Fatalf("restart lost binding: %+v %v", entries, err)
			}
			if _, err := after.BindReceipt("a", bound, "generation-one"); err == nil {
				t.Fatal("orphan became locally owned after restart")
			}
			if err := after.Resolve("a", bound, DefinitelyNotSent, "explicit pre-write refusal"); err != nil {
				t.Fatal(err)
			}
			q = NewStore(dir)
			retry, claimed, err := q.ClaimFront("a")
			if err != nil || !claimed || retry.SessionID != bound.SessionID || retry.TransportRequestID != wantID || retry.AttemptID == bound.AttemptID {
				t.Fatalf("retry lost binding: %+v claimed=%v err=%v", retry, claimed, err)
			}
			if _, err := q.BindReceipt("a", bound, "generation-one"); err == nil {
				t.Fatal("stale attempt bound current retry")
			}
			if _, err := q.BindReceipt("a", retry, "generation-two"); err == nil {
				t.Fatal("retry silently rebound generation")
			}
			if _, err := q.BindReceipt("a", retry, "generation-one"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReceiptBindingLegacyAndCorruptFiles(t *testing.T) {
	for _, state := range []DeliveryState{Attempting, Uncertain} {
		dir := t.TempDir()
		q := NewStore(dir)
		e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "old payload", "old-owner-id", time.Now())
		if err != nil || !claimed {
			t.Fatalf("append: %+v %v", e, err)
		}
		if state == Uncertain {
			if err := q.Resolve("a", e, Unverified, "old uncertain attempt"); err != nil {
				t.Fatal(err)
			}
		}
		q = NewStore(dir)
		entries, err := q.Snapshot("a")
		if err != nil || len(entries) != 1 || entries[0].State != state || entries[0].SessionID != "" || entries[0].TransportRequestID != "" {
			t.Fatalf("legacy hold gained evidence: %+v %v", entries, err)
		}
		if _, err := q.BindReceipt("a", entries[0], "today"); err == nil {
			t.Fatal("legacy unresolved hold rebound to today's seat")
		}
	}
	for _, fields := range []string{
		`"session_id":"generation"`,
		`"transport_request_id":"sendq:entry"`,
		`"transport_request_id":"wrong-request","session_id":"generation"`,
		`"transport_request_id":"sendq:entry","session_id":"  "`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.json")
		body := []byte(`{"agent":"a","entries":[{"id":"entry","text":"payload",` + fields + `}]}`)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(dir).Snapshot("a"); err == nil {
			t.Fatalf("corrupt binding accepted: %s", fields)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != string(body) {
			t.Fatalf("corrupt record rewritten: %s %v", after, err)
		}
	}
}

func TestReceiptBindingPersistenceFailureDoesNotInventEvidence(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	e, _, claimed, err := q.AppendAndClaimWithRequestID("a", "payload", "", time.Now())
	if err != nil || !claimed {
		t.Fatalf("admit: %+v %v", e, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "a.json.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := q.BindReceipt("a", e, "generation"); err == nil {
		t.Fatal("binding reported durable success after failed write")
	}
	entries, err := NewStore(dir).Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0] != e {
		t.Fatalf("failed write changed durable attempt: %+v %v", entries, err)
	}
}

func TestReceiptBindingPreservesDigestIdentity(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	t774Queue(t, q, DigestThreshold+1)
	digest, ok, err := q.ClaimDigest("a")
	if err != nil || !ok || digest.RequestID != "" {
		t.Fatalf("routine digest disabled: %+v %v", digest, err)
	}
	bound, err := q.BindReceipt("a", digest, "generation")
	if err != nil || bound.TransportRequestID != "sendq:"+digest.ID {
		t.Fatalf("digest binding: %+v %v", bound, err)
	}
	if err := q.Resolve("a", bound, DefinitelyNotSent, "pre-write failure"); err != nil {
		t.Fatal(err)
	}
	q = NewStore(dir)
	t774Queue(t, q, DigestThreshold+1)
	if _, ok, err := q.ClaimDigest("a"); err != nil || ok {
		t.Fatalf("bound digest reminted: ok=%v err=%v", ok, err)
	}
	if _, err := q.Consolidate("a", "", "replacement", "operator", "reviewed", time.Now()); err == nil {
		t.Fatal("consolidation erased binding")
	}
	if _, _, replaced, err := q.AppendSuperseding("a", "latest", time.Now(), func(Entry) bool { return true }); err != nil || replaced != DigestThreshold+1 {
		t.Fatalf("supersede erased bound digest: replaced=%d err=%v", replaced, err)
	}
	if removed, err := q.DischargePending("a", func(Entry) bool { return true }); err != nil || removed != 1 {
		t.Fatalf("legacy discharge unfolded/removed bound digest: removed=%d err=%v", removed, err)
	}
	retry, ok, err := q.ClaimFront("a")
	if err != nil || !ok || retry.ID != bound.ID || retry.TransportRequestID != bound.TransportRequestID || retry.SessionID != bound.SessionID || retry.Members != bound.Members || retry.Text != bound.Text {
		t.Fatalf("digest identity changed: %+v %v", retry, err)
	}
	if archived, err := q.ReadArchived(strings.Split(bound.Members, ",")[0]); err != nil || archived.Text == "" {
		t.Fatalf("original digest member lost: %+v %v", archived, err)
	}
}

func TestReceiptAppendAndClaimPreservesOrderAndDistinctQuestions(t *testing.T) {
	q := NewStore(t.TempDir())
	first, _, err := q.Append("a", "older pending", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, depth, claimed, err := q.AppendAndClaimWithRequestID("a", "same question", "owner-B", time.Now())
	if err != nil || claimed || depth != 2 || second.State != Pending || second.AttemptID != "" {
		t.Fatalf("overtook pending: %+v depth=%d claimed=%v err=%v", second, depth, claimed, err)
	}
	old, ok, err := q.ClaimFront("a")
	if err != nil || !ok || old.ID != first.ID {
		t.Fatalf("older entry not next: %+v %v", old, err)
	}
	third, _, claimed, err := q.AppendAndClaimWithRequestID("a", "same question", "owner-C", time.Now())
	if err != nil || claimed || third.ID == second.ID || third.RequestID == second.RequestID {
		t.Fatalf("active bypass or identity merge: %+v %v", third, err)
	}
	if _, _, _, err := q.AppendAndClaimWithRequestID("a", "same question", "owner-B", time.Now()); err == nil {
		t.Fatal("duplicate owner request admitted")
	}
	if err := q.Resolve("a", old, Unverified, "unknown"); err != nil {
		t.Fatal(err)
	}
	next, ok, err := q.ClaimFront("a")
	if err != nil || !ok || next.ID != second.ID {
		t.Fatalf("flow past hold lost order: %+v %v", next, err)
	}
}

func TestReceiptAppendAndClaimConcurrentAndRestart(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0
	const count = 12
	for range count {
		wg.Go(func() {
			_, _, claimed, err := q.AppendAndClaimWithRequestID("a", "distinct acceptance", "", time.Now())
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			if claimed {
				claims++
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	entries, err := NewStore(dir).Snapshot("a")
	if err != nil || claims != 1 || len(entries) != count {
		t.Fatalf("claims=%d depth=%d err=%v", claims, len(entries), err)
	}
	// An abandoned attempt is held, never adopted as a new identified send.
	// Existing flow-past semantics still allow a later obligation to proceed.
	q = NewStore(t.TempDir())
	old, _, _, err := q.AppendAndClaimWithRequestID("a", "abandoned", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	q = NewStore(q.Dir())
	newEntry, depth, claimed, err := q.AppendAndClaimWithRequestID("a", "later", "", time.Now())
	if err != nil || !claimed || depth != 2 || newEntry.ID == old.ID {
		t.Fatalf("orphan blocked later admission: %+v depth=%d claimed=%v err=%v", newEntry, depth, claimed, err)
	}
}
