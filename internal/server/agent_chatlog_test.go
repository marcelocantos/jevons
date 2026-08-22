// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// 🎯T367 oracle: sidebar (fleet agent / aside) conversations must rehydrate
// after a page hard-reload AND a daily jevonsd restart through the SAME
// persistence code main chat uses (internal/chatlog, 🎯T30.1) — not a
// browser-side cache. These tests exercise the product HTTP/build paths, and
// the "restart" arm builds a brand-new Server + Registry over the same state
// dir, which is exactly what a daemon bounce leaves behind.

// sidebarFixture wires a Server the way the daemon does for the sidebar path:
// registry + transcript reader + agent send hook.
type sidebarFixture struct {
	dir  string
	srv  *Server
	name string
	sent []string
}

func newSidebarFixture(t *testing.T, dir, name, sessionID string) *sidebarFixture {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if reg.Def(name) == nil {
		if err := reg.Register(claudia.AgentDef{
			Name:      name,
			WorkDir:   dir,
			SessionID: sessionID,
			Purpose:   claudia.PurposeAside,
			Parent:    "jevons",
		}); err != nil {
			t.Fatal(err)
		}
	}
	f := &sidebarFixture{dir: dir, name: name}
	f.srv = New("test", dir)
	f.srv.SetRegistry(reg)
	f.srv.SetTranscriptReader(transcript.NewReader(filepath.Join(dir, "sessions")))
	f.srv.SetAgentSendHook(func(_, text string) (string, error) {
		f.sent = append(f.sent, text)
		return "sent", nil
	})
	return f
}

// send drives the real product path the sidebar composer uses.
func (f *sidebarFixture) send(t *testing.T, text string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(agentSendRequest{Text: text})
	req := httptest.NewRequest(http.MethodPost,
		"/api/agents/"+f.name+"/send", strings.NewReader(string(body)))
	req.SetPathValue("name", f.name)
	rr := httptest.NewRecorder()
	f.srv.handleAgentSend(rr, req)
	return rr
}

func containsRow(rows []string, want string) bool {
	for _, r := range rows {
		if r == want {
			return true
		}
	}
	return false
}

// TestSidebarConversationSurvivesDaemonRestart is the headline acceptance:
// an owner message typed into the sidebar and the agent's reply are both
// readable from a FRESH Server over the same state dir, with no live process,
// no provider session file, and no browser state.
func TestSidebarConversationSurvivesDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	const name = "att-t367-probe"
	const ownerMsg = "sidebar message must survive a daemon bounce"
	const reply = "acknowledged — working on it"

	// --- daemon life 1 ---
	f := newSidebarFixture(t, dir, name, "sess-t367-probe")
	if rr := f.send(t, ownerMsg); rr.Code != http.StatusOK {
		t.Fatalf("send status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(f.sent) != 1 || f.sent[0] != ownerMsg {
		t.Fatalf("delivery did not happen: %v", f.sent)
	}
	// No sidebar is subscribed: durability must not depend on one being open.
	if f.srv.inspectHasSubscribers(name) {
		t.Fatal("fixture unexpectedly has inspect subscribers")
	}
	f.srv.DeliverInspectLive(name, claudia.Event{
		Type: "assistant", Text: reply, StopReason: "end_turn",
	})

	journalPath := filepath.Join(dir, agentChatLogDirName, name+".jsonl")
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("no durable journal at %s: %v", journalPath, err)
	}
	f.srv.CloseAgentJournals()

	// --- daemon life 2: brand-new Server and Registry over the same state ---
	restarted := newSidebarFixture(t, dir, name, "sess-t367-probe")
	frames := inspectReplay(t, restarted.srv, name)
	rows := replayRoleRows(frames)
	if !containsRow(rows, "user: "+ownerMsg) {
		t.Fatalf("owner message lost across restart: %v", rows)
	}
	if !containsRow(rows, "assistant: "+reply) {
		t.Fatalf("agent reply lost across restart: %v", rows)
	}

	// Hard-reload arm: writeInspectReplay rebuilds from the same journal.
	again := inspectReplay(t, restarted.srv, name)
	if got := strings.Join(replayRoleRows(again), "|"); got != strings.Join(rows, "|") {
		t.Fatalf("reload rebuild drifted:\n%s\n%s", got, strings.Join(rows, "|"))
	}
}

// TestSidebarMessageJournaledBeforeDelivery: the owner's words are durable even
// when delivery fails outright — journal-then-send, as the owner wire does.
func TestSidebarMessageJournaledBeforeDelivery(t *testing.T) {
	dir := t.TempDir()
	const name = "jv-t367.1-worker"
	const ownerMsg = "this must not evaporate when the agent is dead"

	f := newSidebarFixture(t, dir, name, "sess-t367-fail")
	f.srv.SetAgentSendHook(func(_, _ string) (string, error) {
		return "", errNoTranscriptReader // any delivery failure
	})
	if rr := f.send(t, ownerMsg); rr.Code == http.StatusOK {
		t.Fatalf("expected delivery failure, got 200: %s", rr.Body.String())
	}

	rows := replayRoleRows(inspectReplay(t, f.srv, name))
	if !containsRow(rows, "user: "+ownerMsg) {
		t.Fatalf("failed send dropped the owner turn: %v", rows)
	}
}

// TestSidebarJournalDedupesProviderEcho: the ACP echo of a turn we journaled on
// the send path must not paint a second owner bubble.
func TestSidebarJournalDedupesProviderEcho(t *testing.T) {
	dir := t.TempDir()
	const name = "jv-t367.2-echo"
	const ownerMsg = "one bubble only"

	f := newSidebarFixture(t, dir, name, "sess-t367-echo")
	if rr := f.send(t, ownerMsg); rr.Code != http.StatusOK {
		t.Fatalf("send status=%d", rr.Code)
	}
	// Provider echoes the prompt back as a user event.
	f.srv.DeliverInspectLive(name, claudia.Event{Type: "user", Text: ownerMsg})

	rows := replayRoleRows(inspectReplay(t, f.srv, name))
	n := 0
	for _, r := range rows {
		if r == "user: "+ownerMsg {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("owner turn appears %d times: %v", n, rows)
	}
}

// TestAgentJournalFileName keeps hierarchical target ids intact (🎯T197 literal
// dots) while refusing anything that could escape the journal directory.
func TestAgentJournalFileName(t *testing.T) {
	cases := map[string]string{
		"jv-t27.2-config":     "jv-t27.2-config.jsonl",
		"att-mslnnw1t-df1lr7": "att-mslnnw1t-df1lr7.jsonl",
		"jv-t159-seal":        "jv-t159-seal.jsonl",
		"../../etc/passwd":    ".._.._etc_passwd.jsonl",
		"a/b":                 "a_b.jsonl",
		"..":                  "",
		".":                   "",
		"":                    "",
		"   ":                 "",
	}
	for in, want := range cases {
		if got := agentJournalFileName(in); got != want {
			t.Errorf("agentJournalFileName(%q)=%q want %q", in, got, want)
		}
	}
}

// 🎯T481: every provider event is a journal line, including types the
// display mapper used to drop.
func TestJournalRecordsEveryProviderEvent(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	const name = "jv-t481-tape"
	tape := []claudia.Event{
		{Type: "user", Text: "go"},
		{Type: "progress", ProgressType: "tool_use", Raw: []byte(`{"update":{"sessionUpdate":"tool_call_update","title":"Bash"}}`)},
		{Type: "progress", ProgressType: "tool_use", Raw: []byte(`{"update":{"sessionUpdate":"tool_call","title":"Read"}}`)},
		{Type: "assistant", Text: "ok", StopReason: "end_turn"},
		{Type: "system"},
	}
	for _, ev := range tape {
		s.journalAgentEvent(name, ev)
	}
	path := filepath.Join(dir, agentChatLogDirName, name+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) != len(tape) {
		t.Fatalf("journal lines=%d want %d\n%s", len(lines), len(tape), data)
	}
	var lossless, mappedTool int
	for _, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatal(err)
		}
		if m["recorded"] == "lossless" {
			lossless++
		}
		msg, _ := m["message"].(map[string]any)
		raw, _ := msg["content"].([]any)
		for _, blk := range raw {
			b, _ := blk.(map[string]any)
			if b["type"] == "tool_use" && b["name"] == "Read" {
				mappedTool++
			}
		}
	}
	if lossless < 1 {
		t.Fatal("expected a lossless envelope for tool_call_update / bare system")
	}
	if mappedTool != 1 {
		t.Fatalf("mapped tool_use Read count=%d", mappedTool)
	}
}

// TestOverseerTranscriptUnaffectedByAgentJournal: main chat keeps its own
// journal as the single record — no second copy through the agent path.
func TestOverseerTranscriptUnaffectedByAgentJournal(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	s.journalAgentUserTurn(s.overseerAgentName(), "owner words")
	s.journalAgentEvent(s.overseerAgentName(), claudia.Event{Type: "assistant", Text: "reply"})
	if _, err := os.Stat(filepath.Join(dir, agentChatLogDirName)); !os.IsNotExist(err) {
		t.Fatalf("overseer must not get a per-agent journal (err=%v)", err)
	}
}

// TestDeregisteredAgentStillServesJournal is the 🎯T371 server oracle:
// deregistration is not erasure.
//
// The vanish this closes: a fleet aside is stopped and reaped (🎯T165), or is
// removed between the pane painting and its next rehydrate. writeInspectReplay
// still replays the journal for a name missing from the registry.
func TestDeregisteredAgentStillServesJournal(t *testing.T) {
	dir := t.TempDir()
	const name = "att-t371-deregistered"
	const ownerMsg = "does this send?"
	const reply = "yes — and it stays visible"

	f := newSidebarFixture(t, dir, name, "sess-t371")
	if rr := f.send(t, ownerMsg); rr.Code != http.StatusOK {
		t.Fatalf("send status=%d body=%s", rr.Code, rr.Body.String())
	}
	f.srv.DeliverInspectLive(name, claudia.Event{
		Type: "assistant", Text: reply, StopReason: "end_turn",
	})
	f.srv.CloseAgentJournals()

	// Reap the agent the way 🎯T165 auto-deregister does, then rebuild the
	// server over the same state dir with the name absent from the registry.
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove(name); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if reg.Def(name) != nil {
		t.Fatal("agent still registered after Remove")
	}
	srv := New("test", dir)
	srv.SetRegistry(reg)
	srv.SetTranscriptReader(transcript.NewReader(filepath.Join(dir, "sessions")))

	frames := inspectReplay(t, srv, name)
	rows := replayRoleRows(frames)
	if !containsRow(rows, "user: "+ownerMsg) {
		t.Fatalf("owner turn lost after deregistration: %v", rows)
	}
	if !containsRow(rows, "assistant: "+reply) {
		t.Fatalf("agent reply lost after deregistration: %v", rows)
	}

	// A genuinely unknown name (no registry entry, no journal) hydrates
	// as conversation_reset with no turns — not a dump "not found" blob.
	unknown := inspectReplay(t, srv, "att-never-existed")
	if replayUserCount(unknown) != 0 {
		t.Fatalf("unknown name invented turns: %v", replayRoleRows(unknown))
	}
}
