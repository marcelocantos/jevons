// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// ProtocolError is the shape of claudia's broker ProtocolError.
// The production type lives in claudia's broker package, which this
// module does not import. Code is the machine code. The sentence is not.
type ProtocolError struct {
	Code string
	Msg  string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	return "broker protocol: " + e.Code + ": " + e.Msg
}

// notAProtocolError has a Code field and is not a broker refusal.
type notAProtocolError struct {
	Code string
}

func (e *notAProtocolError) Error() string {
	if e == nil {
		return ""
	}
	return "broker protocol: " + e.Code
}

// lineLimitSender is a capable seat whose send returns the error Claudia's
// sendWithRequestID returns: the receipt sentinel and the protocol error
// wrapped together.
type lineLimitSender struct {
	err error
}

func (s *lineLimitSender) Alive() bool { return true }

func (s *lineLimitSender) Interrupt() error { return nil }

func (s *lineLimitSender) Send(string) error { return s.err }

func (s *lineLimitSender) SendWithRequestID(string, string) error { return s.err }

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
	sentence := errors.New("broker protocol: malformed: message exceeds the 1048576-byte line limit")
	if IsBrokerLineLimitError(sentence) {
		t.Fatal("a line-limit sentence is not the refusal")
	}
	malformed := &ProtocolError{Code: "malformed", Msg: "message exceeds the 1048576-byte line limit"}
	if IsBrokerLineLimitError(malformed) {
		t.Fatal("code malformed is not the refusal")
	}
	if IsBrokerLineLimitError(&notAProtocolError{Code: "frame_too_large"}) {
		t.Fatal("a Code field on a different type is not the refusal")
	}
	if IsBrokerLineLimitError(errors.New("agent \"x\" is not running")) {
		t.Fatal("an unrelated error is read as the line limit")
	}
	brokerErr := &ProtocolError{Code: "frame_too_large", Msg: "frame of 2000000 bytes exceeds the 1048576-byte line limit; it was dropped and the connection kept open"}
	if !IsBrokerLineLimitError(brokerErr) {
		t.Fatal("frame_too_large was not recognised")
	}
	wrapped := fmt.Errorf("%w: %w", errors.New("request receipt uncertain"), brokerErr)
	if !IsBrokerLineLimitError(wrapped) {
		t.Fatal("frame_too_large inside a two-error wrap was not recognised")
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

// t661SendDaemon is a daemon whose session roots are the planted t661
// transcript, and whose registry names that seat.
func t661SendDaemon(t *testing.T, name, workdir, sid string, roots discovery.Roots) *Server {
	t.Helper()
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: sid, Provider: "grok", Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg
	s.SetBirthRoots(roots)
	return s
}

// TestT661TypedLineLimitNamesTheSessionOnTheSendPath drives the send that
// dies at the wire. The sender returns the two-error wrap Claudia's
// sendWithRequestID returns. A sentence that only quotes the line limit
// stays a plain send failure.
func TestT661TypedLineLimitNamesTheSessionOnTheSendPath(t *testing.T) {
	const sid = "78239742-0000-4000-8000-0000000230bc"
	const name = "jv-t657-steer-ui"
	workdir := t.TempDir()
	roots := t661Roots(t, workdir, sid)
	pe := &ProtocolError{
		Code: "frame_too_large",
		Msg:  "frame of 2000000 bytes exceeds the 1048576-byte line limit; it was dropped and the connection kept open",
	}
	wrapped := fmt.Errorf("%w: %w", errors.New("request receipt uncertain"), pe)
	s := t661SendDaemon(t, name, workdir, sid, roots)
	_, err := deliverObservedToSender(s, name, "short", false, &lineLimitSender{err: wrapped}, false)
	if err == nil {
		t.Fatal("frame_too_large send returned no error")
	}
	for _, want := range []string{
		"is not the problem",
		name,
		"jevons_agent_kill",
		"jevons_agent_start",
		"Do not retry the send",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("send advice missing %q:\n%s", want, err.Error())
		}
	}

	sentence := errors.New("broker protocol: malformed: message exceeds the 1048576-byte line limit")
	s = t661SendDaemon(t, name, workdir, sid, roots)
	_, err = deliverObservedToSender(s, name, "short", false, &lineLimitSender{err: sentence}, false)
	if err == nil {
		t.Fatal("sentence send returned no error")
	}
	if strings.Contains(err.Error(), "is not the problem") {
		t.Fatalf("a line-limit sentence took the oversized advice path:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "send failed") {
		t.Fatalf("sentence send = %v", err)
	}

	malformed := fmt.Errorf("%w: %w", errors.New("request receipt uncertain"), &ProtocolError{
		Code: "malformed", Msg: "message exceeds the 1048576-byte line limit",
	})
	s = t661SendDaemon(t, name, workdir, sid, roots)
	_, err = deliverObservedToSender(s, name, "short", false, &lineLimitSender{err: malformed}, false)
	if err == nil {
		t.Fatal("malformed send returned no error")
	}
	if strings.Contains(err.Error(), "is not the problem") {
		t.Fatalf("code malformed took the oversized advice path:\n%s", err.Error())
	}
}
