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

// turnTexts flattens a transcript payload into "role: text" rows.
func turnTexts(t *testing.T, payload map[string]any) []string {
	t.Helper()
	raw, ok := payload["turns"].([]map[string]any)
	if !ok {
		if anyTurns, isAny := payload["turns"].([]any); isAny && len(anyTurns) == 0 {
			return nil
		}
		t.Fatalf("turns has unexpected type %T", payload["turns"])
	}
	out := make([]string, 0, len(raw))
	for _, turn := range raw {
		role, _ := turn["role"].(string)
		text, _ := turn["text"].(string)
		out = append(out, role+": "+text)
	}
	return out
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
	payload, ok := restarted.srv.buildAgentTranscriptPayload(name)
	if !ok {
		t.Fatal("agent not found after restart")
	}
	if payload["empty"] == true {
		t.Fatalf("sidebar history empty after restart: %+v", payload)
	}
	rows := turnTexts(t, payload)
	if !containsRow(rows, "user: "+ownerMsg) {
		t.Fatalf("owner message lost across restart: %v", rows)
	}
	if !containsRow(rows, "assistant: "+reply) {
		t.Fatalf("agent reply lost across restart: %v", rows)
	}
	if payload["source"] != conversationSourceAgentJournal {
		t.Fatalf("source=%v want %s", payload["source"], conversationSourceAgentJournal)
	}

	// Hard-reload arm: inspect_subscribe rebuilds from the same call, and a
	// second rebuild is stable (no client state, no drift).
	again, _ := restarted.srv.buildAgentTranscriptPayload(name)
	if got := strings.Join(turnTexts(t, again), "|"); got != strings.Join(rows, "|") {
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

	payload, ok := f.srv.buildAgentTranscriptPayload(name)
	if !ok {
		t.Fatal("agent not found")
	}
	if !containsRow(turnTexts(t, payload), "user: "+ownerMsg) {
		t.Fatalf("failed send dropped the owner turn: %+v", payload["turns"])
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

	payload, _ := f.srv.buildAgentTranscriptPayload(name)
	rows := turnTexts(t, payload)
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

// TestMergeAgentTurns pins the overlay rule: the sidebar shows the union of the
// provider session and the jevons journal, never the intersection.
func TestMergeAgentTurns(t *testing.T) {
	turn := func(n int, role, text string) map[string]any {
		return map[string]any{"turn_number": n, "role": role, "text": text}
	}
	texts := func(ts []map[string]any) string {
		var b strings.Builder
		for i, x := range ts {
			if i > 0 {
				b.WriteString("|")
			}
			b.WriteString(x["role"].(string) + ":" + x["text"].(string))
		}
		return b.String()
	}

	t.Run("no journal keeps session verbatim", func(t *testing.T) {
		session := []map[string]any{turn(1, "user", "a"), turn(1, "assistant", "b")}
		got, used := mergeAgentTurns(session, nil)
		if used || texts(got) != "user:a|assistant:b" {
			t.Fatalf("got=%q used=%v", texts(got), used)
		}
	})

	t.Run("session gone falls back to journal", func(t *testing.T) {
		journal := []map[string]any{turn(1, "user", "a"), turn(1, "assistant", "b")}
		got, used := mergeAgentTurns(nil, journal)
		if !used || texts(got) != "user:a|assistant:b" {
			t.Fatalf("got=%q used=%v", texts(got), used)
		}
	})

	t.Run("unflushed tail appends after session", func(t *testing.T) {
		session := []map[string]any{turn(1, "user", "a"), turn(1, "assistant", "b")}
		journal := []map[string]any{turn(1, "user", "a"), turn(1, "assistant", "b"), turn(2, "user", "c")}
		got, used := mergeAgentTurns(session, journal)
		if !used || texts(got) != "user:a|assistant:b|user:c" {
			t.Fatalf("got=%q used=%v", texts(got), used)
		}
		// Renumbered from 1 at each user boundary.
		if got[2]["turn_number"] != 2 {
			t.Fatalf("turn_number=%v want 2", got[2]["turn_number"])
		}
	})

	t.Run("journal newer than a pre-existing session appends whole", func(t *testing.T) {
		// Agent existed before the journal did: session holds the old turns,
		// the journal only the new one. Nothing overlaps.
		session := []map[string]any{turn(1, "user", "old"), turn(1, "assistant", "older reply")}
		journal := []map[string]any{turn(1, "user", "new")}
		got, used := mergeAgentTurns(session, journal)
		if !used || texts(got) != "user:old|assistant:older reply|user:new" {
			t.Fatalf("got=%q used=%v", texts(got), used)
		}
	})

	t.Run("rotated session shorter than journal uses journal whole", func(t *testing.T) {
		journal := []map[string]any{
			turn(1, "user", "a"), turn(1, "assistant", "b"),
			turn(2, "user", "c"), turn(2, "assistant", "d"),
		}
		session := []map[string]any{turn(1, "user", "c"), turn(1, "assistant", "d")}
		got, used := mergeAgentTurns(session, journal)
		if !used || texts(got) != "user:a|assistant:b|user:c|assistant:d" {
			t.Fatalf("got=%q used=%v", texts(got), used)
		}
	})

	t.Run("session already complete adds nothing", func(t *testing.T) {
		session := []map[string]any{turn(1, "user", "a"), turn(1, "assistant", "b")}
		journal := []map[string]any{turn(1, "user", "a")}
		got, used := mergeAgentTurns(session, journal)
		if used || texts(got) != "user:a|assistant:b" {
			t.Fatalf("got=%q used=%v", texts(got), used)
		}
	})
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
// removed between the pane painting and its next rehydrate. buildAgentTranscriptPayload
// used to return ok=false for any name missing from the registry — BEFORE
// reading the journal — so the wire pushed {"error":"agent not found","turns":[]}
// and the client applied that empty model over the pane, deleting owner turns
// that were sitting durably on disk the whole time (the att-msln9k27 /
// "Discuss T364" class). Send already rehydrates a stopped agent; display now
// matches it.
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

	payload, ok := srv.buildAgentTranscriptPayload(name)
	if !ok {
		t.Fatal("deregistered agent with a journal must still serve its conversation")
	}
	rows := turnTexts(t, payload)
	if !containsRow(rows, "user: "+ownerMsg) {
		t.Fatalf("owner turn lost after deregistration: %v", rows)
	}
	if !containsRow(rows, "assistant: "+reply) {
		t.Fatalf("agent reply lost after deregistration: %v", rows)
	}
	if empty, _ := payload["empty"].(bool); empty {
		t.Fatal("payload reported empty while carrying journal turns")
	}
	if unreg, _ := payload["unregistered"].(bool); !unreg {
		t.Fatal("payload must mark the agent unregistered so the UI can say so")
	}
	if src, _ := payload["source"].(string); src != conversationSourceAgentJournal {
		t.Fatalf("source=%q want %q", src, conversationSourceAgentJournal)
	}

	// A genuinely unknown name (no registry entry, no journal) is still
	// not-found — this must not become a catch-all that invents panes.
	if _, ok := srv.buildAgentTranscriptPayload("att-never-existed"); ok {
		t.Fatal("unknown name with no journal must remain not-found")
	}
}
