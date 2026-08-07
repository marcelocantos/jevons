// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/thread"
)

// failFleet drives a provider outage through the butler direct/deliver paths:
// sendErr models a transport-level failure, reply models Grok ACP delivering
// the outage as the assistant turn (🎯T283).
type failFleet struct {
	alive   map[string]bool
	sendErr error
	reply   string
}

func (f *failFleet) Launch(t *thread.Thread) error {
	if t.SessionID == "" {
		t.SessionID = "sess-fail"
	}
	if f.alive == nil {
		f.alive = map[string]bool{}
	}
	f.alive[t.ID] = true
	return nil
}

func (f *failFleet) Send(id, text string) (string, error) {
	if f.sendErr != nil {
		return "", f.sendErr
	}
	return f.reply, nil
}

func (f *failFleet) Alive(id string) bool { return f.alive[id] }
func (f *failFleet) Stop(id string)       { f.alive[id] = false }
func (f *failFleet) Remove(id string)     { delete(f.alive, id) }

func directServer(t *testing.T, ff butler.Fleet) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	b := newPushButler(t, dir, ff, nil)
	if _, err := b.Spawn(butler.SpawnArgs{ID: "w1", WorkDir: dir, Parent: "jevons"}); err != nil {
		t.Fatal(err)
	}
	return &Server{butler: b}, "w1"
}

func callDirect(t *testing.T, s *Server, id, text string) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": id, "text": text}
	res, err := s.handleThreadDirect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// 🎯T283 acceptance: an outage during jevons_thread_direct returns the
// classified reason, not a bare "Internal error" the caller cannot act on.
func TestThreadDirectClassifiesTransportOutage(t *testing.T) {
	s, id := directServer(t, &failFleet{sendErr: fmt.Errorf("acp session/prompt: Internal error")})
	res := callDirect(t, s, id, "do the thing")
	if !res.IsError {
		t.Fatal("outage must surface as an error result")
	}
	got := toolText(res)
	if !strings.Contains(got, string(agenterr.ClassBackendUnavailable)) {
		t.Fatalf("reply must name the class, got %q", got)
	}
	if got == "Internal error" {
		t.Fatal("bare Internal error still reaching the caller")
	}
}

// The Grok ACP shape: the outage is the assistant turn, so Direct returns it
// as a successful reply. That is the J10 failure mode — the caller reads an
// outage as "the shell tool did not run".
func TestThreadDirectClassifiesReplyCarriedOutage(t *testing.T) {
	s, id := directServer(t, &failFleet{reply: "Internal error"})
	res := callDirect(t, s, id, "run the shell command")
	if !res.IsError {
		t.Fatal("reply-carried outage must surface as an error result")
	}
	if !strings.Contains(toolText(res), string(agenterr.ClassBackendUnavailable)) {
		t.Fatalf("got %q", toolText(res))
	}
}

// The guard that keeps the fix honest: a real worker reply passes through
// untouched even when it talks about errors and timeouts.
func TestThreadDirectPassesRealWorkThrough(t *testing.T) {
	reply := "J10_SHELL_OK\nDone — fixed the timeout handling and committed abc1234."
	s, id := directServer(t, &failFleet{reply: reply})
	res := callDirect(t, s, id, "run the shell command")
	if res.IsError {
		t.Fatalf("real work must not be classified as a failure: %s", toolText(res))
	}
	if toolText(res) != reply {
		t.Fatalf("reply altered:\n got %q\nwant %q", toolText(res), reply)
	}
}

// A missing thread is a caller error, not a provider failure — wording that
// existing callers match on must survive.
func TestThreadDirectLeavesNonProviderErrorsAlone(t *testing.T) {
	s, _ := directServer(t, &failFleet{})
	res := callDirect(t, s, "ghost", "hi")
	if !res.IsError {
		t.Fatal("want error for unknown thread")
	}
	if !strings.Contains(toolText(res), "no thread") {
		t.Fatalf("got %q", toolText(res))
	}
}

// 🎯T283 acceptance: the deliver path (jevons_event_push) classifies too.
func TestEventPushClassifiesOutage(t *testing.T) {
	dir := t.TempDir()
	ff := &failFleet{sendErr: fmt.Errorf("session/prompt failed: 503 Service Unavailable")}
	b := newPushButler(t, dir, ff, nil)
	if _, err := b.Spawn(butler.SpawnArgs{ID: "w1", WorkDir: dir, Parent: "jevons"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{butler: b}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": "w1", "event": "ci", "text": "green"}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("outage must surface as an error result")
	}
	if !strings.Contains(toolText(res), string(agenterr.ClassBackendUnavailable)) {
		t.Fatalf("got %q", toolText(res))
	}
}

// 🎯T283: jwork must not report an outage as "Worker finished (no output)".
func TestJworkFailureReportsOutageOverEmptyResult(t *testing.T) {
	class, msg := jworkFailure(agenterr.ClassBackendUnavailable, "Internal error", "")
	if class != agenterr.ClassBackendUnavailable {
		t.Fatalf("class = %v, want backend_unavailable", class)
	}
	if !strings.Contains(msg, string(agenterr.ClassBackendUnavailable)) {
		t.Errorf("owner copy must name the class, got %q", msg)
	}
}

// A reply that is nothing but the outage counts even without an error event —
// the provider delivered it as the worker's whole turn.
func TestJworkFailureReportsReplyCarriedOutage(t *testing.T) {
	class, _ := jworkFailure(agenterr.ClassNone, "", "Internal error")
	if class != agenterr.ClassBackendUnavailable {
		t.Fatalf("class = %v, want backend_unavailable", class)
	}
}

// An error event followed by real output is a recovered run, not an outage.
func TestJworkFailureKeepsRecoveredRun(t *testing.T) {
	class, msg := jworkFailure(agenterr.ClassRateLimit, "429 rate limit",
		"Retried after backoff. Done — wrote the file and committed abc1234.")
	if class.IsFailure() {
		t.Fatalf("recovered run reported as failure: class=%v msg=%q", class, msg)
	}
}

func TestJworkFailureCleanRun(t *testing.T) {
	if class, _ := jworkFailure(agenterr.ClassNone, "", "All tests pass."); class.IsFailure() {
		t.Fatalf("clean run reported as failure: %v", class)
	}
}

// toolFailure passes non-provider errors through verbatim so existing callers
// and their assertions keep working.
func TestToolFailurePreservesNonProviderErrors(t *testing.T) {
	res := toolFailure("test", "w1", fmt.Errorf("id and text are required"))
	if !res.IsError || toolText(res) != "id and text are required" {
		t.Fatalf("got %q", toolText(res))
	}
	if toolFailure("test", "w1", nil) != nil {
		t.Error("nil error must yield no result")
	}
}
