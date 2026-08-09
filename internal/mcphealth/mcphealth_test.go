// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcphealth

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"
)

// liveListener starts a real TCP listener and returns its http URL. The
// listener accepts and drops connections; the probe only needs the SYN/ACK.
func liveListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return fmt.Sprintf("http://%s/mcp", ln.Addr().String())
}

// closedPortURL binds a port, learns its number, then releases it, so the
// address is one nothing is listening on — exactly the shape of the live
// `jevonsmcp-journey` leak (a port a prior process used and gave back).
func closedPortURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return fmt.Sprintf("http://%s/mcp", addr)
}

// 🎯T379 acceptance 4, the load-bearing pair: a registration pointing at a
// closed port is reported dead, and one pointing at a live server is not.
//
// Both halves are required. The dead half alone passes against a classifier
// that calls everything dead; the live half alone passes against the
// pre-fix behaviour of noticing nothing. Only together do they pin the
// distinction.
func TestProbeDistinguishesDeadFromLive(t *testing.T) {
	dead := Probe(Registration{Name: "journey", Transport: "http", URL: closedPortURL(t)}, DefaultTimeout)
	if dead.Status != StatusDead {
		t.Errorf("closed port: got status %q (%s), want %q",
			dead.Status, dead.Detail, StatusDead)
	}

	live := Probe(Registration{Name: "daemon", Transport: "http", URL: liveListener(t)}, DefaultTimeout)
	if live.Status != StatusLive {
		t.Errorf("live listener: got status %q (%s), want %q",
			live.Status, live.Detail, StatusLive)
	}
	if live.Dead() {
		t.Errorf("live listener must never be reported dead: %+v", live)
	}
}

// Acceptance 2: dead must be distinguishable from merely slow. A timeout
// proves nothing, so it must not be laundered into "dead" — otherwise the
// first slow-starting server would be reported as a fault and the signal
// would be worthless.
func TestSlowIsNotDead(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Status
	}{
		{"connected", nil, StatusLive},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, StatusDead},
		{"nxdomain", &net.DNSError{Err: "no such host", IsNotFound: true}, StatusDead},
		{"timeout", &net.OpError{Op: "dial", Err: timeoutErr{}}, StatusSlow},
		{"dns timeout", &net.DNSError{Err: "timeout", IsTimeout: true}, StatusSlow},
		{"novel error", errors.New("something else entirely"), StatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, detail := ClassifyDialErr(tc.err)
			if got != tc.want {
				t.Errorf("ClassifyDialErr(%v) = %q (%s), want %q", tc.err, got, detail, tc.want)
			}
			if detail == "" {
				t.Errorf("every verdict needs a human-readable detail; got empty for %v", tc.err)
			}
		})
	}
}

// A blackholed address (TEST-NET-1, RFC 5737 — routed nowhere) times out
// rather than refusing, and must come back slow. This is the real-network
// counterpart to the injected timeout above: it is the case an
// over-broad "anything that fails is dead" classifier gets wrong.
func TestBlackholeAddressIsSlowNotDead(t *testing.T) {
	if testing.Short() {
		t.Skip("network timeout probe")
	}
	res := Probe(Registration{
		Name: "blackhole", Transport: "http", URL: "http://192.0.2.1:13715/mcp",
	}, 300*time.Millisecond)
	if res.Dead() {
		t.Errorf("unroutable address timed out and must not be called dead: %+v", res)
	}
	if res.Status != StatusSlow {
		t.Logf("note: got %q (%s) — acceptable if the host refused fast", res.Status, res.Detail)
	}
}

func TestDialAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://127.0.0.1:13715/mcp", "127.0.0.1:13715"},
		{"http://127.0.0.1:13705/mcp", "127.0.0.1:13705"},
		{"http://example.test/mcp", "example.test:80"},
		{"https://example.test/mcp", "example.test:443"},
	}
	for _, tc := range cases {
		got, err := DialAddress(tc.in)
		if err != nil {
			t.Errorf("DialAddress(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DialAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := DialAddress("not a url at all"); err == nil {
		t.Error("want error for an unparseable endpoint")
	}
}

// stdio entries have no endpoint; probing them would report a fault for
// every correctly configured command-transport server on the machine.
func TestStdioIsSkippedNotDead(t *testing.T) {
	res := Probe(Registration{Name: "sawmill", Transport: "stdio"}, DefaultTimeout)
	if res.Status != StatusSkipped {
		t.Errorf("stdio: got %q, want %q", res.Status, StatusSkipped)
	}
	if res.Dead() {
		t.Error("stdio registration must never be reported dead")
	}
}

// The exact document shape that produced this target: a user-scope map
// carrying both the daemon's real endpoint and a leaked journey-suite one.
const claudeConfigFixture = `{
  "mcpServers": {
    "jevonsmcp":         {"type": "http", "url": "http://127.0.0.1:13705/mcp"},
    "jevonsmcp-journey": {"type": "http", "url": "http://127.0.0.1:13715/mcp"},
    "sawmill":           {"command": "/opt/homebrew/bin/mcpbridge", "args": ["connect"]}
  },
  "projects": {
    "/some/other/repo": {
      "mcpServers": {"projectonly": {"type": "http", "url": "http://127.0.0.1:19999/mcp"}}
    }
  }
}`

func TestParseClaudeRegistrations(t *testing.T) {
	regs, err := ParseClaudeRegistrations([]byte(claudeConfigFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(regs) != 3 {
		t.Fatalf("want 3 user-scope registrations, got %d: %+v", len(regs), regs)
	}
	// Sorted by name, so the order is pinned.
	if regs[0].Name != "jevonsmcp" || regs[1].Name != "jevonsmcp-journey" {
		t.Errorf("want sorted names, got %q, %q", regs[0].Name, regs[1].Name)
	}
	if regs[1].URL != "http://127.0.0.1:13715/mcp" || regs[1].Transport != "http" {
		t.Errorf("journey entry not parsed: %+v", regs[1])
	}
	// A command entry with no explicit type is stdio, and must not be
	// mistaken for an http entry with an empty URL.
	if regs[2].Name != "sawmill" || regs[2].Transport != "stdio" {
		t.Errorf("command entry should infer stdio: %+v", regs[2])
	}
	if Probeable(regs[2]) {
		t.Error("stdio entry must not be probeable")
	}
	// Project-scoped servers are out of scope: they are not carried by
	// every agent, so including them would report faults for repos the
	// agent will never open.
	for _, r := range regs {
		if r.Name == "projectonly" {
			t.Error("project-scoped server must not appear in user-scope registrations")
		}
	}
}

// Acceptance 1: the fault has to be *said*, not merely computed. Report is
// the surfacing seam — and it must stay quiet when nothing is wrong, or the
// note becomes noise that trains readers to ignore it.
func TestReportNamesDeadAndStaysQuietOtherwise(t *testing.T) {
	results := []Result{
		{Registration: Registration{Name: "jevonsmcp", URL: "http://127.0.0.1:13705/mcp"}, Status: StatusLive},
		{Registration: Registration{Name: "jevonsmcp-journey", URL: "http://127.0.0.1:13715/mcp"},
			Status: StatusDead, Detail: "connection refused — nothing is listening on that port"},
		{Registration: Registration{Name: "slowone", URL: "http://127.0.0.1:1/mcp"}, Status: StatusSlow},
	}
	got := Report(results)
	if !strings.Contains(got, "jevonsmcp-journey") || !strings.Contains(got, "13715") {
		t.Errorf("report must name the dead registration and its URL: %q", got)
	}
	if strings.Contains(got, "slowone") {
		t.Errorf("a slow server is not a fault and must not be reported: %q", got)
	}
	// "jevonsmcp" is a substring of "jevonsmcp-journey", so check the
	// healthy entry is absent by counting, not by Contains.
	if strings.Count(got, "13705") != 0 {
		t.Errorf("healthy registration must not be listed: %q", got)
	}

	healthy := []Result{
		{Registration: Registration{Name: "jevonsmcp"}, Status: StatusLive},
		{Registration: Registration{Name: "sawmill"}, Status: StatusSkipped},
		{Registration: Registration{Name: "slowone"}, Status: StatusSlow},
	}
	if q := Report(healthy); q != "" {
		t.Errorf("no dead registrations must produce no note, got %q", q)
	}
}

// End-to-end over the fixture with a real closed port substituted in: parse
// → probe → report, the whole path an agent start exercises.
func TestFixtureConfigSurfacesTheDeadEntry(t *testing.T) {
	doc := strings.ReplaceAll(claudeConfigFixture,
		"http://127.0.0.1:13705/mcp", liveListener(t))
	doc = strings.ReplaceAll(doc,
		"http://127.0.0.1:13715/mcp", closedPortURL(t))

	regs, err := ParseClaudeRegistrations([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	results := ProbeAll(regs, DefaultTimeout)
	dead := Dead(results)
	if len(dead) != 1 {
		t.Fatalf("want exactly the journey entry dead, got %d: %+v", len(dead), results)
	}
	if dead[0].Name != "jevonsmcp-journey" {
		t.Errorf("wrong entry reported dead: %+v", dead[0])
	}
	if Report(results) == "" {
		t.Error("a dead registration must produce a note")
	}
}

// timeoutErr is a net.Error whose Timeout() is true, for the injected case.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
