// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// t774Fill queues 50 mixed entries for one seat, shaped like the claudia-po
// specimen of 2026-09-21: worker reports, held-backlog relays, worker-idle
// events and free-form supervisor messages.
func t774Fill(t *testing.T, s *Server, seat string) (senders []string) {
	t.Helper()
	q := s.sendQueue()
	at := time.Now().Add(-time.Hour)
	add := func(text string) {
		at = at.Add(time.Minute)
		if _, _, err := q.Append(seat, text, at); err != nil {
			t.Fatal(err)
		}
	}
	// 20 reports from 5 workers, four each.
	for i := 0; i < 4; i++ {
		for w := 1; w <= 5; w++ {
			add(fmt.Sprintf("[Agent w%d responded] report_id=2026092%dT0%d0000Z-%02d%02d\nw%d status round %d", w, i, i, w, i, w, i))
		}
	}
	// 10 worker-idle events: two per worker w1..w5.
	for i := 0; i < 2; i++ {
		for w := 1; w <= 5; w++ {
			add(fmt.Sprintf("[event: worker-idle] A work agent entered phase=idle.\n\nWorker: w%d\nTarget: T%d\nParent: %s\n\nround %d", w, w, seat, i))
		}
	}
	// 9 held-backlog relays: 3 for w1 (which has reports: folded), 6 for
	// h6 (no reports: latest survives).
	for i := 0; i < 3; i++ {
		add(fmt.Sprintf("[held backlog from w1 — that seat was reaped, so this is routed to you as its parent (🎯T582)]\n\n[event: idle-nudge] NUDGE w1 %d", i))
	}
	for i := 0; i < 6; i++ {
		add(fmt.Sprintf("[held backlog from h6 — that seat was reaped, so this is routed to you as its parent (🎯T582)]\n\n[event: idle-nudge] NUDGE h6 %d", i))
	}
	// 11 free-form messages from distinct authors.
	for i := 0; i < 11; i++ {
		add(fmt.Sprintf("SUPERVISOR probe-%d: unique free-form message %d", i, i))
	}
	return []string{"w1", "w2", "w3", "w4", "w5", "h6"}
}

func TestT774FiftyMixedEntriesDrainAsOneDigest(t *testing.T) {
	s, sender, _ := t418Daemon(t, t.TempDir())
	const seat = "claudia-po"
	senders := t774Fill(t, s, seat)
	if n := observedPendingSends(s, seat); n != 50 {
		t.Fatalf("setup queued %d entries, want 50", n)
	}

	s.drainAgentSendQueue(seat)

	got := sender.delivered()
	if len(got) != 1 {
		t.Fatalf("submits = %d, want exactly one digest for 50 entries", len(got))
	}
	digest := got[0]
	for _, name := range senders {
		if !strings.Contains(digest, name) {
			t.Errorf("digest does not name sender %q:\n%s", name, digest)
		}
	}
	// Newest state per sender survives in full; the newest w3 report is round 3.
	if !strings.Contains(digest, "w3 status round 3") {
		t.Errorf("digest lost the newest w3 report")
	}
	// Superseded worker-idle: only the latest per worker; round 0 text is gone
	// from the body.
	if strings.Contains(digest, "round 0") && strings.Count(digest, "entered phase=idle") > 5 {
		t.Errorf("superseded worker-idle notices were not collapsed")
	}
	// Held backlog for w1 (re-reported) is folded; h6's latest survives.
	if strings.Contains(digest, "NUDGE w1") {
		t.Errorf("held backlog for a re-reported seat was not folded into its report")
	}
	if !strings.Contains(digest, "NUDGE h6 5") || strings.Contains(digest, "NUDGE h6 0") {
		t.Errorf("held backlog for h6 should keep only its latest")
	}
	// Free-form messages are never dropped.
	for i := 0; i < 11; i++ {
		if !strings.Contains(digest, fmt.Sprintf("unique free-form message %d", i)) {
			t.Errorf("free-form message %d disappeared", i)
		}
	}
	// The digest says what it collapsed, and how many.
	if !strings.Contains(digest, "collapsed") || !strings.Contains(digest, "50") {
		t.Errorf("digest does not state what was collapsed:\n%s", digest)
	}
	if n := observedPendingSends(s, seat); n != 0 {
		t.Fatalf("queue not empty after digest delivery: %d", n)
	}

	// Nothing disappears silently: an original text stays readable by id.
	id := t774SupersededReportID(t, digest)
	orig, err := s.sendQueue().ReadArchived(id)
	if err != nil || !strings.Contains(orig.Text, "[Agent w") || !strings.Contains(orig.Text, "status round") {
		t.Fatalf("archived original %q unreadable: %v %+v", id, err, orig)
	}
	res, err := s.handleSendqReconcile(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"name": seat, "action": "read", "entry_id": id},
	}})
	if err != nil || res.IsError {
		t.Fatalf("read action failed: %v %+v", err, res)
	}
}

// A small backlog keeps one-message-at-a-time delivery: a digest is for a
// pile, not for two messages.
func TestT774SmallBacklogIsNotDigested(t *testing.T) {
	s, sender, _ := t418Daemon(t, t.TempDir())
	q := s.sendQueue()
	for i := 0; i < sendq.DigestThreshold; i++ {
		if _, _, err := q.Append("a", fmt.Sprintf("msg %d", i), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	s.drainAgentSendQueue("a")
	// One message per turn boundary, as before: the first goes out verbatim.
	if got := sender.delivered(); len(got) != 1 || got[0] != "msg 0" {
		t.Fatalf("small backlog was digested: %v", got)
	}
	if n := observedPendingSends(s, "a"); n != sendq.DigestThreshold-1 {
		t.Fatalf("remaining = %d", n)
	}
}

// T726/T766.5: a held uncertain attempt is never swept into a digest.
func TestT774HeldEntryIsNotDigested(t *testing.T) {
	s, sender, _ := t418Daemon(t, t.TempDir())
	q := s.sendQueue()
	held, _, _ := q.Append("a", "held uncertain payload", time.Now())
	e, ok, err := q.ClaimFront("a")
	if err != nil || !ok || e.ID != held.ID {
		t.Fatalf("claim: %v %v", err, ok)
	}
	if err := q.Resolve("a", e, sendq.Unverified, "test"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		q.Append("a", fmt.Sprintf("SUPERVISOR n%d: body", i), time.Now())
	}
	s.drainAgentSendQueue("a")
	if len(sender.delivered()) != 1 {
		t.Fatalf("submits = %d, want 1", len(sender.delivered()))
	}
	entries, _ := q.Snapshot("a")
	if len(entries) != 1 || entries[0].ID != held.ID || entries[0].State != sendq.Uncertain {
		t.Fatalf("held attempt disturbed: %+v", entries)
	}
	if strings.Contains(sender.delivered()[0], "held uncertain payload") {
		t.Fatal("held payload leaked into digest")
	}
}

// t774SupersededReportID pulls the id of a collapsed report out of the digest.
func t774SupersededReportID(t *testing.T, digest string) string {
	t.Helper()
	const marker = "readable by entry id: "
	i := strings.Index(digest, marker)
	if i < 0 {
		t.Fatalf("digest names no collapsed report ids:\n%s", digest)
	}
	rest := digest[i+len(marker):]
	if j := strings.IndexAny(rest, " ,\n"); j > 0 {
		rest = rest[:j]
	}
	return rest
}
