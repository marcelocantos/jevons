// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// 🎯T381 — the server tells the browser WHO spoke a user-role turn.
//
// The owner reported an agent's seal report painted as raw source: literal
// "**Commit:**", literal "## Oracle evidence", an unrendered pipe table. The
// browser had no way to know better, because every user-role line looked the
// same on the wire whether the owner typed it or an agent sent it.
//
// The fix is provenance, not a heuristic. These tests hold both halves: the
// field is present and correct, AND owner turns are never marked as agent
// turns — because the moment an owner turn is misfiled, the owner's own
// asterisks get eaten, which is a worse defect than the one being fixed.

func wireTurnOrigin(t *testing.T, line string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("wire line is not JSON: %v\nline: %s", err, line)
	}
	v, ok := m[wireTurnOriginKey]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s is %T, want string\nline: %s", wireTurnOriginKey, v, line)
	}
	return s
}

func wireUserContent(t *testing.T, line string) string {
	t.Helper()
	var m struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("wire line is not JSON: %v\nline: %s", err, line)
	}
	if m.Type != "user" || m.Message.Role != "user" {
		t.Fatalf("not a user-role line: %s", line)
	}
	// Either content shape (🎯T384 typed blocks, or a legacy bare string) —
	// this test is about provenance, not about which shape carries the text.
	return wireContentText(m.Message.Content)
}

// The owner's real report body, trimmed — markdown that must survive the trip
// unrewritten. The server classifies the turn; it never edits it.
const t381ReportBody = "🎯T22 SEALED\n\n**Commit:** `bec51ca`\n\n## Oracle evidence\n\n| Criterion | Status |\n| --- | --- |\n| 1 | green |"

func TestUserWireLineCarriesTurnOrigin(t *testing.T) {
	t.Run("owner turn is marked owner", func(t *testing.T) {
		line := chatUserEcho("why is **this** not rendering?")
		if got := wireTurnOrigin(t, line); got != sendOriginOwner {
			t.Fatalf("turn_origin = %q, want %q — an unmarked owner turn is how "+
				"the owner's literal asterisks get eaten downstream", got, sendOriginOwner)
		}
		if got := wireUserContent(t, line); got != "why is **this** not rendering?" {
			t.Fatalf("owner text was rewritten: %q", got)
		}
	})

	t.Run("agent report is marked agent", func(t *testing.T) {
		line := chatUserEchoAs(t381ReportBody, sendOriginAgent)
		if got := wireTurnOrigin(t, line); got != sendOriginAgent {
			t.Fatalf("turn_origin = %q, want %q — without it the browser paints "+
				"the report as raw source (the reported defect)", got, sendOriginAgent)
		}
		if got := wireUserContent(t, line); got != t381ReportBody {
			t.Fatalf("report text was rewritten by the wire layer:\n got %q\nwant %q", got, t381ReportBody)
		}
	})

	t.Run("anything unrecognised falls back to owner, never agent", func(t *testing.T) {
		// Verbatim is the safe default. A caller that has not been taught about
		// provenance — or a garbled value — must degrade to "leave the text
		// alone", not to "run it through a markdown renderer".
		for _, origin := range []string{"", "human", "Owner", "system", "AGENT-ish"} {
			line := chatUserEchoAs("literal ** and ## and | stay", origin)
			if got := wireTurnOrigin(t, line); got != sendOriginOwner {
				t.Fatalf("origin %q → turn_origin %q, want %q", origin, got, sendOriginOwner)
			}
		}
	})
}

// A turn addressed TO a fleet agent carries no userTurnPrefix marker, so its
// journal line is the only place provenance can live. Without this, every
// report one agent sends another replays in the sidebar as raw source — the
// same defect the owner saw in main chat, on a different surface.
func TestAgentJournalUserTurnCarriesOrigin(t *testing.T) {
	dir := t.TempDir()
	j := newAgentJournals(dir)

	j.appendUserAs("jv-t22-seal", "please seal T22 when the probe is green", sendOriginOwner)
	j.appendUserAs("jv-t22-seal", t381ReportBody, sendOriginAgent)

	l := j.logFor("jv-t22-seal")
	if l == nil {
		t.Fatal("journal was not created")
	}
	var lines, user []string
	if err := l.Replay(func(ln string) error {
		lines = append(lines, ln)
		if strings.Contains(ln, `"type":"user"`) {
			user = append(user, ln)
		}
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(user) != 2 {
		t.Fatalf("journaled %d user lines, want 2:\n%s", len(user), strings.Join(lines, "\n"))
	}
	if got := wireTurnOrigin(t, user[0]); got != sendOriginOwner {
		t.Fatalf("owner turn journaled as %q, want %q", got, sendOriginOwner)
	}
	if got := wireTurnOrigin(t, user[1]); got != sendOriginAgent {
		t.Fatalf("agent report journaled as %q, want %q — the sidebar cannot "+
			"tell a report from the owner's words without it", got, sendOriginAgent)
	}
	if got := wireUserContent(t, user[1]); got != t381ReportBody {
		t.Fatalf("report text was rewritten on the journal path:\n got %q\nwant %q", got, t381ReportBody)
	}
}
