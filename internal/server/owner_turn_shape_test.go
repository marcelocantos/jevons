// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T384: owner turns persist in the SAME shape as agent turns.
//
// The owner's live complaint: "I type into an aside, press Enter, it seems to
// send, then it disappears." Evidence from his own journal,
// ~/.jevons/agent-chatlogs/att-msln9k27-nf4y87.jsonl — 9 assistant records and
// 1 owner record, and the owner's is the odd one out:
//
//	owner:     {"message":{"content":"park this. …","role":"user"}}
//	assistant: {"message":{"content":[{"text":"…","type":"text"}],"role":"assistant"}}
//
// content is a BARE STRING for the owner and a LIST OF TYPED BLOCKS for the
// agent. Every consumer that walks content blocks and keeps type=="text" —
// which is the shape the whole family is written against — yields nothing for
// the owner's own words. That is the vanishing text.
//
// The fix writes one shape and reads both: owner turns are journaled as typed
// blocks like every other turn, and the readers still accept the bare string so
// the owner's existing history (every chatlog on disk today) keeps rendering.
// Acceptance 4 is the load-bearing half — a shape fix that stranded his
// history would trade one vanish for a worse one.

// ownerTurnBlockText pulls text out of a wire line's message.content assuming
// the BLOCK shape only — deliberately strict, because it is the shape-sensitive
// consumer this target exists to satisfy. If content is a bare string this
// returns "", which is exactly how the owner's text used to disappear.
func ownerTurnBlockText(t *testing.T, line string) string {
	t.Helper()
	var frame struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		t.Fatalf("wire line is not JSON: %v (%s)", err, line)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(frame.Message.Content, &blocks); err != nil {
		// A bare string lands here — the pre-fix owner record.
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type != "text" {
			continue
		}
		b.WriteString(blk.Text)
	}
	return b.String()
}

// userBlockText reads the owner's words out of an already-decoded
// message.content value in EITHER shape (🎯T384): the typed blocks written
// today, or the bare string every pre-T384 journal on disk still holds. The
// sibling shape assertions in this package call it so they state the contract
// ("the owner's words are readable") rather than one encoding of it.
func userBlockText(content any) string {
	var b strings.Builder
	appendBlock := func(blk map[string]any) {
		if kind, _ := blk["type"].(string); kind != "text" {
			return
		}
		s, _ := blk["text"].(string)
		b.WriteString(s)
	}
	switch c := content.(type) {
	case string:
		return c
	case []any:
		for _, raw := range c {
			if blk, ok := raw.(map[string]any); ok {
				appendBlock(blk)
			}
		}
	case []map[string]any:
		for _, blk := range c {
			appendBlock(blk)
		}
	}
	return b.String()
}

// readJournalLines returns the raw wire lines of an agent's journal.
func readJournalLines(t *testing.T, dir, name string) []string {
	t.Helper()
	j := newAgentJournals(dir)
	path := j.path(name)
	if path == "" {
		t.Fatalf("no journal path for %q", name)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines = append(lines, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan journal: %v", err)
	}
	return lines
}

// TestOwnerTurnPersistsInBlockShape is acceptance 1: the owner's turn, written
// through the real send path, carries content as typed blocks — the same shape
// the agent's own reply uses — so a block-walking consumer renders it.
//
// RED before the fix: chatUserEcho wrote "content": text (a bare string), so
// ownerTurnBlockText returned "" and the owner's words were unreachable to
// every consumer built for the assistant shape.
func TestOwnerTurnPersistsInBlockShape(t *testing.T) {
	dir := t.TempDir()
	const name = "att-t384-shape"
	const ownerMsg = "does this survive a reload?"

	f := newSidebarFixture(t, dir, name, "sess-t384-shape")
	if rr := f.send(t, ownerMsg); rr.Code != http.StatusOK {
		t.Fatalf("send status=%d body=%s", rr.Code, rr.Body.String())
	}
	f.srv.CloseAgentJournals()

	lines := readJournalLines(t, dir, name)
	if len(lines) == 0 {
		t.Fatal("owner turn was not journaled at all")
	}

	var ownerLine string
	for _, ln := range lines {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(ln), &probe); err != nil {
			continue
		}
		if probe.Type == "user" {
			ownerLine = ln
			break
		}
	}
	if ownerLine == "" {
		t.Fatalf("no user-role record in journal: %v", lines)
	}

	// The heart of 🎯T384: a consumer that walks typed blocks — the shape every
	// agent turn already uses — must find the owner's words.
	if got := ownerTurnBlockText(t, ownerLine); got != ownerMsg {
		t.Fatalf("owner turn is not block-shaped: block-walking consumer read %q, want %q\nline: %s",
			got, ownerMsg, ownerLine)
	}
}

// TestOwnerAndAgentTurnsShareContentShape is the symmetry assertion behind the
// target's name. Rather than hardcoding "a list", it demands that the owner's
// record and the agent's record agree — whatever shape the family settles on,
// one consumer must serve both.
//
// RED before the fix: owner content unmarshalled as a string, agent content as
// an array, so the shapes disagreed.
func TestOwnerAndAgentTurnsShareContentShape(t *testing.T) {
	dir := t.TempDir()
	const name = "att-t384-symmetry"
	const ownerMsg = "shapes must match"
	const reply = "agreed"

	f := newSidebarFixture(t, dir, name, "sess-t384-symmetry")
	if rr := f.send(t, ownerMsg); rr.Code != http.StatusOK {
		t.Fatalf("send status=%d body=%s", rr.Code, rr.Body.String())
	}
	f.srv.DeliverInspectLive(name, claudia.Event{
		Type: "assistant", Text: reply, StopReason: "end_turn",
	})
	f.srv.CloseAgentJournals()

	shapes := map[string]string{}
	for _, ln := range readJournalLines(t, dir, name) {
		var frame struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ln), &frame); err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(frame.Message.Content))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		shape := "blocks"
		if trimmed[0] == '"' {
			shape = "bare-string"
		}
		if prev, ok := shapes[frame.Type]; ok && prev != shape {
			t.Fatalf("%s records disagree with themselves: %s vs %s", frame.Type, prev, shape)
		}
		shapes[frame.Type] = shape
	}

	if shapes["user"] == "" || shapes["assistant"] == "" {
		t.Fatalf("need both roles journaled, got %v", shapes)
	}
	if shapes["user"] != shapes["assistant"] {
		t.Fatalf("owner turns persist as %s while agent turns persist as %s — "+
			"a consumer written for one drops the other (🎯T384)",
			shapes["user"], shapes["assistant"])
	}
}

// TestLegacyBareStringOwnerTurnStillRenders is acceptance 4, and it is the
// half that protects the owner's existing history. Every agent chatlog on disk
// today holds bare-string owner content; the reader must keep rendering them
// even though the writer no longer produces that shape.
//
// This test is GREEN before the fix too — deliberately. It is the ratchet that
// stops the shape flip from being implemented as a swap.
func TestLegacyBareStringOwnerTurnStillRenders(t *testing.T) {
	const legacy = "park this. claude remote suffices for now."

	// The exact record shape from the owner's att-msln9k27-nf4y87.jsonl.
	lines := []string{
		`{"type":"user","timestamp":"2026-08-09T11:32:25.879763Z",` +
			`"message":{"role":"user","content":` + jsonQuote(legacy) + `}}`,
		`{"type":"assistant","timestamp":"2026-08-09T11:32:31.352624Z",` +
			`"message":{"role":"assistant","content":[{"type":"text","text":"Already done."}]}}`,
	}

	turns := overseerTurnsFromWire(lines)
	var sawOwner bool
	for _, tn := range turns {
		if role, _ := tn["role"].(string); role != "user" {
			continue
		}
		if text, _ := tn["text"].(string); text == legacy {
			sawOwner = true
		}
	}
	if !sawOwner {
		t.Fatalf("legacy bare-string owner turn stopped rendering — the fix "+
			"stranded the owner's existing history (acceptance 4): %v", turns)
	}
}

// TestLiveOwnerFrameIsBlockShaped covers the OTHER writer. The journal is only
// half the owner's experience: while a sidebar is open, the pane is fed by
// inspectLiveEvent frames straight off DeliverInspectLive, not by a journal
// replay. That writer had the identical asymmetry — a bare string for the
// owner, typed blocks for the agent — so a block-walking consumer of the live
// wire dropped the owner's turn even when the journal held it correctly.
//
// The assertion is deliberately strict about the shape rather than routing
// through the shape-agnostic reader: a reader that accepts both shapes (which
// ours does, on purpose, for acceptance 4) can never fail on a bare string, so
// only a direct shape check can hold this writer to the contract.
func TestLiveOwnerFrameIsBlockShaped(t *testing.T) {
	const msg = "does the live pane keep this?"

	ev, ok := inspectLiveEvent(claudia.Event{Type: "user", Text: msg})
	if !ok {
		t.Fatal("owner text produced no live frame at all")
	}
	m, _ := ev["message"].(map[string]any)
	if _, isString := m["content"].(string); isString {
		t.Fatalf("live owner frame carries a bare string — a block-walking "+
			"consumer of the live wire reads '' and the owner's turn never "+
			"reaches the pane (🎯T384): %v", m["content"])
	}
	if got := userBlockText(m["content"]); got != msg {
		t.Fatalf("live owner frame block text = %q, want %q", got, msg)
	}

	// Symmetry with the agent's own live frame — one consumer, both roles.
	aev, ok := inspectLiveEvent(claudia.Event{Type: "assistant", Text: "sure"})
	if !ok {
		t.Fatal("assistant text produced no live frame")
	}
	am, _ := aev["message"].(map[string]any)
	if _, isString := am["content"].(string); isString {
		t.Fatal("assistant live frame is a bare string; the roles have diverged")
	}
}

// TestBlockShapedOwnerTurnRoundTripsThroughReader is acceptance 5's round-trip:
// the shape the writer now produces must come back out of the reader as the
// owner's words, not as empty text.
func TestBlockShapedOwnerTurnRoundTripsThroughReader(t *testing.T) {
	const msg = "block-shaped owner turn"
	lines := []string{
		`{"type":"user","timestamp":"2026-08-09T12:00:00Z",` +
			`"message":{"role":"user","content":[{"type":"text","text":` + jsonQuote(msg) + `}]}}`,
	}
	turns := overseerTurnsFromWire(lines)
	if len(turns) == 0 {
		t.Fatal("block-shaped owner turn produced no turns")
	}
	text, _ := turns[0]["text"].(string)
	if text != msg {
		t.Fatalf("round-trip lost the owner's words: got %q want %q", text, msg)
	}
}

// jsonQuote JSON-quotes a string for the fixture literals above.
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
