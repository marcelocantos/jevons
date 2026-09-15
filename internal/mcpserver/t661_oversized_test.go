// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// t661Roots plants a Grok session for sid under a temp root with one
// ordinary turn and one record over the broker line limit.
func t661Roots(t *testing.T, workdir, sid string) discovery.Roots {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, discovery.EncodeCWDBucket(workdir), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", transcript.BrokerLineLimit+512)
	body := `{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"go"}}}}` + "\n" +
		`{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + big + `"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return discovery.Roots{GrokSessions: root}
}

// 🎯T661 acceptance: agent_list marks the seat as oversized-session.
func TestT661SeatWithOversizedSessionIsMarked(t *testing.T) {
	const sid = "78239742-0000-4000-8000-0000000230bc"
	workdir := t.TempDir()
	roots := t661Roots(t, workdir, sid)
	d := claudia.AgentDef{Name: "jv-t661-seat", WorkDir: workdir, SessionID: sid, Provider: "grok"}
	s := &Server{}

	lines := s.seatOversized(d, roots)
	if len(lines) != 1 || lines[0].Line != 2 {
		t.Fatalf("oversized census = %+v, want one record at line 2", lines)
	}
	// Second read is served from the cache keyed by size and mtime.
	if again := s.seatOversized(d, roots); len(again) != 1 {
		t.Fatalf("cached census = %+v", again)
	}
	line := FormatOversizedSeatLine(d.Name, lines)
	for _, want := range []string{"oversized-session", "line 2", "jevons_agent_kill", "jevons_agent_start", "🎯T661"} {
		if !strings.Contains(line, want) {
			t.Fatalf("agent_list line missing %q: %s", want, line)
		}
	}

	// A small session is a single stat, never a scan, and is not marked.
	small := claudia.AgentDef{Name: "jv-t661-small", WorkDir: workdir, SessionID: "11111111-0000-4000-8000-000000000001", Provider: "grok"}
	if got := s.seatOversized(small, roots); got != nil {
		t.Fatalf("missing session marked oversized: %+v", got)
	}
}

// 🎯T661 acceptance: a send that dies at the broker wire names the seat's
// oversized records and the recovery, not the bare protocol error.
func TestT661SendErrorNamesTheSessionAndRecovery(t *testing.T) {
	brokerErr := errors.New("broker protocol: malformed: message exceeds the 1048576-byte line limit")
	if !IsBrokerLineLimitError(brokerErr) {
		t.Fatal("the broker refusal is not recognised")
	}
	if IsBrokerLineLimitError(errors.New("agent \"x\" is not running")) {
		t.Fatal("an unrelated error is read as the line limit")
	}
	lines := []transcript.OversizedLine{{Line: 41, Bytes: 1_400_000}, {Line: 58, Bytes: 1_200_000}}
	msg := OversizedSendAdvice("jv-t657-steer-ui", 1200, lines, brokerErr)
	for _, want := range []string{
		"1200-byte payload is not the problem",
		"2 record(s) over 1048576 bytes",
		"line 41 (1400000 bytes)",
		"jevons_agent_kill \"jv-t657-steer-ui\"",
		"jevons_agent_start",
		"Do not retry the send",
		"🎯T661",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("advice missing %q:\n%s", want, msg)
		}
	}
}
