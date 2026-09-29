// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"strings"
	"testing"
	"time"
)

// 🎯T804: jevons_agent_list never reads a seat transcript on the caller's
// time. On 2026-09-22 the first calls after a restart took over 30 s while
// the T797 seat-MCP check scanned transcripts to warm its cache. The request
// path reads only that cache; a cold entry starts a background scan.
func TestT804AgentListNeverScansTranscriptsOnTheRequestPath(t *testing.T) {
	e := t797Setup(t, t797Transcript("jevonsmcp"))
	// A large transcript, as a long-lived seat has.
	path := e.plantTranscript(t797Transcript("jevonsmcp") + strings.Repeat(`{"type":"assistant","timestamp":"2026-09-20T08:00:02Z","message":{"content":"work"}}`+"\n", 20000))
	seatMCPCache.Delete(path)
	e.clock.add(SeatMCPGrace + time.Minute)

	var warmed []string
	seatMCPWarm = func(p string) { warmed = append(warmed, p) }
	t.Cleanup(func() { seatMCPWarm = seatMCPWarmDefault })

	before := seatMCPScans.Load()
	start := time.Now()
	out := e.list()
	took := time.Since(start)
	if n := seatMCPScans.Load() - before; n != 0 {
		t.Fatalf("agent_list read the transcript on the request path (%d scans)", n)
	}
	if len(warmed) == 0 || warmed[0] != path {
		t.Fatalf("a cold entry did not ask for a background scan: %v", warmed)
	}
	if strings.Contains(out, "mcp-missing") {
		t.Fatalf("a cold cache must say nothing yet, not guess:\n%s", out)
	}
	// 🎯T97 exemption: an upper bound on a call that reads no transcript; a
	// slow host can only make it pass more slowly, never fail a correct one.
	if took > 5*time.Second {
		t.Fatalf("cold agent_list took %s", took)
	}
	// The background scan warms the cache; the next call answers from it.
	if _, err := scanSeatMCP(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.list(), "mcp-missing: "+e.name) {
		t.Fatal("after the background scan, agent_list does not report the missing server")
	}
}

// A growing transcript is read from where the last scan stopped.
func TestT804SeatMCPScanIsIncremental(t *testing.T) {
	e := t797Setup(t, t797Transcript("jevonsmcp"))
	path := e.plantTranscript(t797Transcript("jevonsmcp"))
	seatMCPCache.Delete(path)
	if _, err := scanSeatMCP(path); err != nil {
		t.Fatal(err)
	}
	first, _ := seatMCPCache.Load(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	late := t797Line(t797Start.Add(time.Hour), `{"type":"deferred_tools_delta","addedNames":["mcp__bullseye__bullseye_query"]}`)
	if _, err := f.WriteString(late); err != nil {
		t.Fatal(err)
	}
	f.Close()
	seen, err := scanSeatMCP(path)
	if err != nil {
		t.Fatal(err)
	}
	if !seen.servers["bullseye"] || !seen.servers["jevonsmcp"] {
		t.Fatalf("servers after append = %v", seen.servers)
	}
	entry, _ := seatMCPCache.Load(path)
	fi, _ := os.Stat(path)
	if got := entry.(seatMCPCacheEntry).offset; got != fi.Size() {
		t.Fatalf("offset %d, want the whole file %d", got, fi.Size())
	}
	if first.(seatMCPCacheEntry).offset >= entry.(seatMCPCacheEntry).offset {
		t.Fatal("the offset did not advance")
	}
}
