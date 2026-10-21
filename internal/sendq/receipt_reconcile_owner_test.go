// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"testing"
	"time"
)

// Requeuing an older ambiguous delivery must not displace the owner of a
// later send that flowed past it. Exercise the real reconciliation boundary,
// then both drain strategies, with enough pending messages to form a digest.
func TestReceiptRequeuePreservesLaterActiveOwner(t *testing.T) {
	for _, strategy := range []string{"front", "digest"} {
		t.Run(strategy, func(t *testing.T) {
			q := NewStore(t.TempDir())
			now := time.Now()
			a, _, claimed, err := q.AppendAndClaimWithRequestID("a", "older hold", "", now)
			if err != nil || !claimed {
				t.Fatalf("claim A: %+v claimed=%v err=%v", a, claimed, err)
			}
			if err := q.Resolve("a", a, Unverified, "transport outcome unknown"); err != nil {
				t.Fatal(err)
			}
			b, _, claimed, err := q.AppendAndClaimWithRequestID("a", "active send", "", now)
			if err != nil || !claimed {
				t.Fatalf("claim B: %+v claimed=%v err=%v", b, claimed, err)
			}
			b, err = q.BindReceipt("a", b, "generation")
			if err != nil {
				t.Fatal(err)
			}
			for range DigestThreshold {
				if _, _, err := q.Append("a", "later pending", now); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := q.Reconcile("a", a.ID, a.AttemptID, ReconcileRequeue, "reviewer", "receiver proved non-delivery", now); err != nil {
				t.Fatal(err)
			}
			claim := q.ClaimFront
			if strategy == "digest" {
				claim = q.ClaimDigest
			}
			if e, ok, err := claim("a"); err != nil || ok {
				t.Fatalf("claimed past active B: %+v ok=%v err=%v", e, ok, err)
			}
			if again, err := q.BindReceipt("a", b, "generation"); err != nil || again != b {
				t.Fatalf("B lost ownership: %+v err=%v", again, err)
			}
			if err := q.Resolve("a", b, Confirmed, "receiver accepted"); err != nil {
				t.Fatal(err)
			}
			// Once B settles, the ordinary drain can take the older obligation.
			e, ok, err := claim("a")
			if err != nil || !ok {
				t.Fatalf("drain remained blocked: %+v ok=%v err=%v", e, ok, err)
			}
			if strategy == "front" && e.ID != a.ID {
				t.Fatalf("requeue order changed: got %s want %s", e.ID, a.ID)
			}
			if strategy == "digest" && e.Members == "" {
				t.Fatal("digest control did not form a digest")
			}
		})
	}
}
