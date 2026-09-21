// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func t774Queue(t *testing.T, s *Store, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, _, err := s.Append("a", fmt.Sprintf("SUPERVISOR m%d: body", i), time.Now().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestT774DigestNeedsMoreThanAHandful(t *testing.T) {
	s := NewStore(t.TempDir())
	t774Queue(t, s, DigestThreshold)
	if _, ok, err := s.ClaimDigest("a"); err != nil || ok {
		t.Fatalf("threshold-sized queue digested: ok=%v err=%v", ok, err)
	}
}

// A digest whose send provably never landed goes back to Pending; the next
// claim unpacks it into its members instead of nesting a digest in a digest.
func TestT774RetriedDigestUnpacksInsteadOfNesting(t *testing.T) {
	for _, durable := range []bool{true, false} {
		dir := ""
		if durable {
			dir = t.TempDir()
		}
		s := NewStore(dir)
		t774Queue(t, s, 8)
		d1, ok, err := s.ClaimDigest("a")
		if err != nil || !ok {
			t.Fatalf("claim: %v %v", err, ok)
		}
		if err := s.Resolve("a", d1, DefinitelyNotSent, "no live process"); err != nil {
			t.Fatal(err)
		}
		t774Queue(t, s, 2) // more arrive meanwhile
		d2, ok, err := s.ClaimDigest("a")
		if err != nil || !ok {
			t.Fatalf("second claim: %v %v", err, ok)
		}
		if strings.Contains(d2.Text, "[digest of") && strings.Count(d2.Text, "[digest of") > 1 {
			t.Fatalf("digest nested inside digest:\n%s", d2.Text)
		}
		if !strings.Contains(d2.Text, "10 queued message(s)") {
			t.Fatalf("second digest lost members:\n%s", d2.Text)
		}
	}
}

// An unverifiable digest stays held for reconciliation with its members'
// originals still readable (🎯T726: nothing silently dropped).
func TestT774UncertainDigestStaysHeldAndReadable(t *testing.T) {
	s := NewStore(t.TempDir())
	t774Queue(t, s, 9)
	d, ok, err := s.ClaimDigest("a")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", err, ok)
	}
	if err := s.Resolve("a", d, Unverified, "window closed"); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Snapshot("a")
	if len(entries) != 1 || entries[0].State != Uncertain || entries[0].Members == "" {
		t.Fatalf("held digest: %+v", entries)
	}
	first := strings.Split(entries[0].Members, ",")[0]
	if e, err := s.ReadArchived(first); err != nil || !strings.Contains(e.Text, "m0") {
		t.Fatalf("original unreadable: %v %+v", err, e)
	}
	// A later claim flows past the held digest, never re-offering it.
	if _, ok, _ := s.ClaimDigest("a"); ok {
		t.Fatal("held digest was offered again")
	}
}
