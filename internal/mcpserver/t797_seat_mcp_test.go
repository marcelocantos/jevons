// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T797 oracle: a Claude seat whose transcript shows a required MCP server
// never attached is flagged in agent_list and its parent is told once, within
// SeatMCPGrace. Observation source: the seat's own session records
// (deferred_tools_delta / mcp_instructions_delta attachments), not a new probe.

var t797Start = time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

func t797Line(ts time.Time, attachment string) string {
	return fmt.Sprintf(`{"type":"attachment","timestamp":%q,"attachment":%s}`+"\n",
		ts.Format(time.RFC3339), attachment)
}

// t797Transcript is a seat that started at t797Start; attached lists the
// servers whose tools appeared in a delta.
func t797Transcript(attached ...string) string {
	names := `"Bash","Read"`
	instr := []string{}
	for _, a := range attached {
		names += fmt.Sprintf(`,"mcp__%s__tool"`, a)
		instr = append(instr, fmt.Sprintf("%q", a))
	}
	out := t797Line(t797Start, `{"type":"deferred_tools_delta","addedNames":[`+names+`]}`)
	out += t797Line(t797Start.Add(time.Second),
		`{"type":"mcp_instructions_delta","addedNames":[`+strings.Join(instr, ",")+`]}`)
	return out
}

func t797Setup(t *testing.T, transcript string) *t679_2Env {
	t.Helper()
	e := t679_2Harness(t, claudia.ProviderClaude, t679_2SID)
	cfg := `{"mcpServers":{"bullseye":{"type":"http","url":"http://127.0.0.1:1/mcp"}}}`
	if err := os.WriteFile(filepath.Join(e.home, ".claude.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	d := *e.reg.Def(e.name)
	d.MCPServers = []claudia.MCPServer{{Name: "jevonsmcp", Type: "http", URL: "http://127.0.0.1:1/mcp"}}
	if err := e.reg.Register(d); err != nil {
		t.Fatal(err)
	}
	e.plantTranscript(transcript)
	e.clock.t = t797Start
	return e
}

func (e *t679_2Env) mcpNotices() []string {
	var out []string
	for _, m := range e.parent.sent {
		if strings.Contains(m, "mcp-missing:") && strings.Contains(m, e.name) {
			out = append(out, m)
		}
	}
	return out
}

func TestT797MissingRequiredServerFlaggedAndParentTold(t *testing.T) {
	e := t797Setup(t, t797Transcript("jevonsmcp")) // bullseye never attached
	e.clock.add(SeatMCPGrace - time.Second)
	if strings.Contains(e.list(), "mcp-missing") {
		t.Fatal("flagged inside the grace window")
	}
	e.clock.add(2 * time.Second)
	out := e.list()
	if !strings.Contains(out, "mcp-missing: "+e.name) || !strings.Contains(out, "bullseye") {
		t.Fatalf("agent_list does not mark the seat missing bullseye:\n%s", out)
	}
	if strings.Contains(out, "missing jevonsmcp") || strings.Contains(out, "jevonsmcp,") {
		t.Fatalf("attached server reported missing:\n%s", out)
	}
	e.list()
	e.s.sweepSeatMCP()
	if n := e.mcpNotices(); len(n) != 1 {
		t.Fatalf("parent notices = %d, want exactly 1: %v", len(n), n)
	}
}

func TestT797AllAttachedIsQuiet(t *testing.T) {
	e := t797Setup(t, t797Transcript("jevonsmcp", "bullseye"))
	e.clock.add(SeatMCPGrace + time.Hour)
	if out := e.list(); strings.Contains(out, "mcp-missing") {
		t.Fatalf("healthy seat flagged:\n%s", out)
	}
	if n := e.mcpNotices(); len(n) != 0 {
		t.Fatalf("healthy seat notified: %v", n)
	}
}

func TestT797LateAttachClearsFlag(t *testing.T) {
	e := t797Setup(t, t797Transcript("jevonsmcp"))
	e.clock.add(SeatMCPGrace + time.Minute)
	if !strings.Contains(e.list(), "mcp-missing") {
		t.Fatal("expected flag")
	}
	body := t797Transcript("jevonsmcp") +
		t797Line(t797Start.Add(SeatMCPGrace), `{"type":"deferred_tools_delta","addedNames":["mcp__bullseye__bullseye_query"]}`)
	e.plantTranscript(body)
	if out := e.list(); strings.Contains(out, "mcp-missing") {
		t.Fatalf("flag survived the server attaching:\n%s", out)
	}
}

func TestT797UnobservableIsNotFlagged(t *testing.T) {
	// No delta record at all (nothing to compare against) and non-Claude
	// providers prove nothing.
	e := t797Setup(t, `{"type":"user","timestamp":"2026-09-20T08:00:00Z"}`+"\n")
	e.clock.add(SeatMCPGrace + time.Hour)
	if out := e.list(); strings.Contains(out, "mcp-missing") {
		t.Fatalf("flagged with no delta observation:\n%s", out)
	}
}
