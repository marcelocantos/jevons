// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/sendq"
)

func t826WorkerReport(worker, body string) string {
	line := agentRespondedRoutingLine(worker, agentreport.Handle{Agent: worker, ReportID: "t826"}, time.Time{})
	return envelope.PrefixAgentResponded + "\n" + line + "\n" + body
}

func t826StatusPing(worker, note string) string {
	return t826WorkerReport(worker, fmt.Sprintf("```jevons\njevons: kind status-ping\njevons: status in-progress\n```\n\n%s", note))
}

func t826FinishReport(worker string) string {
	body := "```jevons\njevons: kind finish-report\njevons: target T826\njevons: oracle sha=abcdef0123456\njevons: silent-ledger none\n```\n"
	return t826WorkerReport(worker, body)
}

func t826GateStatusPing(worker string) string {
	body := "```jevons\njevons: kind status-ping\njevons: status in-progress\njevons: gate-id=cafebabe\n```\n"
	return t826WorkerReport(worker, body)
}

// 🎯T826: N non-terminal reports from one worker leave one pending; a terminal
// between them is kept; a report carrying gate-id is kept.
func TestT826NonTerminalReportsCollapseInSendq(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	const po = "jevons-po"
	const worker = "jv-t826-a"
	for i := 0; i < 5; i++ {
		if _, err := s.enqueueAgentSend(po, t826StatusPing(worker, fmt.Sprintf("waiting round %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.sendQueue().Snapshot(po)
	if err != nil {
		t.Fatal(err)
	}
	fromWorker := 0
	for _, e := range entries {
		if strings.Contains(e.Text, worker) {
			fromWorker++
		}
	}
	if fromWorker != 1 {
		t.Fatalf("pending reports from %s = %d, want 1; queue %+v", worker, fromWorker, entries)
	}
	if !strings.Contains(entries[len(entries)-1].Text, "waiting round 4") {
		t.Fatalf("newest progress missing: %q", entries[len(entries)-1].Text)
	}

	if _, err := s.enqueueAgentSend(po, t826FinishReport(worker)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.enqueueAgentSend(po, t826StatusPing(worker, "after finish")); err != nil {
		t.Fatal(err)
	}
	entries, err = s.sendQueue().Snapshot(po)
	if err != nil {
		t.Fatal(err)
	}
	var finish, after bool
	for _, e := range entries {
		if strings.Contains(e.Text, "kind finish-report") {
			finish = true
		}
		if strings.Contains(e.Text, "after finish") {
			after = true
		}
	}
	if !finish || !after {
		t.Fatalf("terminal between non-terminal: finish=%v after=%v queue %+v", finish, after, entries)
	}

	if _, err := s.enqueueAgentSend(po, t826GateStatusPing(worker)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.enqueueAgentSend(po, t826StatusPing(worker, "plain ping")); err != nil {
		t.Fatal(err)
	}
	entries, err = s.sendQueue().Snapshot(po)
	if err != nil {
		t.Fatal(err)
	}
	gate := 0
	for _, e := range entries {
		if strings.Contains(e.Text, "gate-id=cafebabe") {
			gate++
		}
	}
	if gate != 1 {
		t.Fatalf("gate-id report count = %d, want 1", gate)
	}
}

func TestT826DigestKeepsTerminalBetweenNonTerminal(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	const po = "jevons-po"
	const worker = "jv-t826-b"
	q := s.sendQueue()
	at := time.Now().Add(-time.Hour)
	add := func(text string) {
		at = at.Add(time.Minute)
		if _, _, err := q.Append(po, text, at); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		add(t826StatusPing(worker, fmt.Sprintf("progress %d", i)))
	}
	add(t826FinishReport(worker))
	for i := 0; i < 3; i++ {
		add(t826StatusPing(worker, fmt.Sprintf("after %d", i)))
	}
	for i := 0; i < sendq.DigestThreshold; i++ {
		add(fmt.Sprintf("SUPERVISOR filler-%d: free-form", i))
	}
	s.drainAgentSendQueue(po)
	entries, err := q.Snapshot(po)
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Text, "digest of") {
		t.Fatalf("digest handoff = %+v %v", entries, err)
	}
	digest := entries[0].Text
	if !strings.Contains(digest, "kind finish-report") {
		t.Fatalf("digest dropped terminal report:\n%s", digest)
	}
	if strings.Contains(digest, "progress 0") || strings.Contains(digest, "progress 1") {
		t.Fatalf("digest should collapse early non-terminal stack:\n%s", digest)
	}
	if !strings.Contains(digest, "after 2") {
		t.Fatalf("digest should keep newest post-finish progress:\n%s", digest)
	}
	if !strings.Contains(digest, "earlier non-terminal report(s) from "+worker) {
		t.Fatalf("digest should state per-worker non-terminal collapse:\n%s", digest)
	}
}
