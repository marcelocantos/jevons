// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package panecensus

import (
	"strings"
	"testing"
)

// 2026-08-15 census shape: 48 registered, 21 unregistered idle orphans,
// 16 idle claudia-pool-* warm spares. 48+21+16 = 85 panes.
func t459Census() (panes []Pane, names map[string]bool) {
	names = map[string]bool{}
	for i := 0; i < 48; i++ {
		name := "jv-reg-" + itoa(i)
		names[name] = true
		panes = append(panes, testPane(name, "%r"+itoa(i), name, FlightIdle))
	}
	for i := 0; i < 21; i++ {
		panes = append(panes, testPane("orphan-"+itoa(i), "%o"+itoa(i), "", FlightIdle))
	}
	for i := 0; i < 16; i++ {
		panes = append(panes, testPane("claudia-pool-"+itoa(i), "%p"+itoa(i), "", FlightIdle))
	}
	return panes, names
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func reapIDs(r Report) map[string]bool {
	out := map[string]bool{}
	for _, d := range r.Reap() {
		out[d.Pane.ID] = true
	}
	return out
}

func TestT459ReapsOnlyUnregisteredIdle(t *testing.T) {
	panes, names := t459Census()
	// One unregistered pane is mid-turn — the working-agent control.
	mid := Pane{Window: "orphan-midturn", ID: "%mid", Title: "esc to interrupt"}
	panes = append(panes, mid.WithFlight(FlightInFlight))

	r := Plan(panes, names, DefaultWarmPoolMax)
	got := reapIDs(r)

	for i := 0; i < 48; i++ {
		id := "%r" + itoa(i)
		if got[id] {
			t.Errorf("registered pane %s was reaped", id)
		}
	}
	for i := 0; i < 21; i++ {
		id := "%o" + itoa(i)
		if !got[id] {
			t.Errorf("unregistered idle orphan %s was kept", id)
		}
	}
	if got["%mid"] {
		t.Fatal("unregistered mid-turn pane was reaped — the in-flight check is the load-bearing clause")
	}

	// Warm pool: 16 idle, bound 2 → 14 reaped, 2 kept.
	var poolReaped, poolKept int
	for i := 0; i < 16; i++ {
		id := "%p" + itoa(i)
		if got[id] {
			poolReaped++
		} else {
			poolKept++
		}
	}
	if poolKept != DefaultWarmPoolMax {
		t.Errorf("warm-pool kept %d, want bound %d", poolKept, DefaultWarmPoolMax)
	}
	if poolReaped != 16-DefaultWarmPoolMax {
		t.Errorf("warm-pool reaped %d, want %d", poolReaped, 16-DefaultWarmPoolMax)
	}
	if r.Orphans != 21 {
		t.Errorf("orphans = %d, want 21", r.Orphans)
	}
	if r.Registered != 48 {
		t.Errorf("registered = %d, want 48", r.Registered)
	}
	if r.Cost.Processes != 48*(1+TypicalChildrenPerAgent) {
		t.Errorf("cost processes = %d, want %d", r.Cost.Processes, 48*(1+TypicalChildrenPerAgent))
	}
}

// Mutation: a Plan that ignores in-flight state reaps the working agent
// and must go RED. The shipped Plan is the control that stays green.
func testPane(window, id, agent string, f Flight) Pane {
	return Pane{Window: window, ID: id, AgentName: agent, Flight: f, flightSet: true}
}

func TestT459MutationDroppingInFlightCheckKillsWorkingAgent(t *testing.T) {
	panes := []Pane{
		testPane("jv-live", "%live", "jv-live", FlightIdle),
		testPane("orphan-idle", "%idle", "", FlightIdle),
		testPane("orphan-busy", "%busy", "", FlightInFlight),
	}
	names := map[string]bool{"jv-live": true}

	// Shipped classifier: only the idle orphan.
	r := Plan(panes, names, DefaultWarmPoolMax)
	got := reapIDs(r)
	if !got["%idle"] || got["%busy"] || got["%live"] {
		t.Fatalf("shipped Plan reaped %v, want only %%idle", keys(got))
	}

	// Mutant: treat every unregistered pane as idle.
	mutant := reapIgnoringFlight(panes, names)
	if !mutant["%busy"] {
		t.Fatal("mutant did not reap the mid-turn pane — this test is not detecting the mutation")
	}
	if mutant["%live"] {
		t.Fatal("mutant reaped a registered pane; over-broad")
	}
}

func reapIgnoringFlight(panes []Pane, names map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, p := range panes {
		if registered(p, names) {
			continue
		}
		out[p.ID] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	var s []string
	for k := range m {
		s = append(s, k)
	}
	return s
}

func TestT459UnknownFlightIsNotReaped(t *testing.T) {
	r := Plan([]Pane{testPane("mystery", "%u", "", FlightUnknown)}, nil, DefaultWarmPoolMax)
	if len(r.Reap()) != 0 {
		t.Fatalf("unknown-flight pane was reaped: %+v", r.Reap())
	}
}

func TestT459CostAndCensusAreVisible(t *testing.T) {
	c := EstimateCost(48)
	line := FormatCost(c)
	if !strings.Contains(line, "48 agents") || !strings.Contains(line, "🎯T459") {
		t.Fatalf("cost line %q does not name the agent count", line)
	}
	if c.Processes != 48*16 {
		t.Errorf("48 × 16 processes = %d, want 768", c.Processes)
	}
	panes, names := t459Census()
	r := Plan(panes, names, DefaultWarmPoolMax)
	census := FormatCensus(r, DefaultWarmPoolMax)
	for _, want := range []string{"85 panes", "48 registered", "21 reapable", "bound 2"} {
		if !strings.Contains(census, want) {
			t.Errorf("census %q missing %q", census, want)
		}
	}
}

func TestT459WarmPoolBoundIsStatedNotEmergent(t *testing.T) {
	if DefaultWarmPoolMax <= 0 {
		t.Fatal("warm-pool bound must be a positive stated number")
	}
	var panes []Pane
	for i := 0; i < 5; i++ {
		panes = append(panes, Pane{Window: "claudia-pool-" + itoa(i), ID: "%" + itoa(i)}.WithFlight(FlightIdle))
	}
	r := Plan(panes, nil, 2)
	if r.PoolKept != 2 || r.PoolReaped != 3 {
		t.Fatalf("pool kept=%d reaped=%d, want 2/3", r.PoolKept, r.PoolReaped)
	}
}

// Live 2026-08-18 daily-driver shape (🎯T514): registry holds jevons-po
// plus the full Claude session UUID; the pane window is SessionWindowName
// of that UUID; AgentName and the list-panes session-id field are empty.
func t514LivePO() (Pane, map[string]bool) {
	sid := "377bf9c3-6483-4f01-a642-fe5a3030248e"
	p := Pane{Window: "claudia-377bf9c3", ID: "%2"}.WithFlight(FlightIdle)
	names := map[string]bool{
		"jevons-po": true,
		sid:         true,
	}
	return p, names
}

func TestT514LiveClaudeSessionPaneIsNotAnOrphan(t *testing.T) {
	p, names := t514LivePO()
	r := Plan([]Pane{
		p,
		Pane{Window: "orphan-idle", ID: "%idle"}.WithFlight(FlightIdle),
		Pane{Window: "claudia-deadbeef", ID: "%dead"}.WithFlight(FlightIdle),
	}, names, DefaultWarmPoolMax)
	got := reapIDs(r)
	if got["%2"] {
		t.Fatal("live Claude PO pane claudia-377bf9c3 was reaped — T514 identity miss")
	}
	if !got["%idle"] {
		t.Fatal("unrelated idle orphan was kept")
	}
	if !got["%dead"] {
		t.Fatal("leftover claudia-<hex> with no matching registry session was kept")
	}
	if r.Registered != 1 {
		t.Fatalf("registered=%d, want 1 (the live PO)", r.Registered)
	}
}

func TestT514ExactNameAndSessionIDStillMatch(t *testing.T) {
	names := map[string]bool{"jevons-po": true, "377bf9c3-6483-4f01-a642-fe5a3030248e": true}
	byName := Pane{Window: "jevons-po", ID: "%n", AgentName: "jevons-po"}.WithFlight(FlightIdle)
	bySID := Pane{Window: "other", ID: "%s", SessionID: "377bf9c3-6483-4f01-a642-fe5a3030248e"}.WithFlight(FlightIdle)
	r := Plan([]Pane{byName, bySID}, names, DefaultWarmPoolMax)
	if n := len(r.Reap()); n != 0 {
		t.Fatalf("pre-T514 exact matches were reaped: %+v", r.Reap())
	}
}

func TestT514WarmPoolPrefixIsNotASessionWindow(t *testing.T) {
	// claudia-pool-* must stay on the pool path even if a session id
	// happens to start with "pool-".
	names := map[string]bool{"pool-00000000-dead-beef-cafe-000000000000": true}
	p := Pane{Window: "claudia-pool-0", ID: "%p0"}.WithFlight(FlightIdle)
	r := Plan([]Pane{p}, names, 2)
	if r.PoolKept != 1 || r.Registered != 0 {
		t.Fatalf("pool pane classified registered=%d poolKept=%d, want 0/1", r.Registered, r.PoolKept)
	}
}

func TestT514SessionWindowNameMatchesClaudia(t *testing.T) {
	if got := SessionWindowName("377bf9c3-6483-4f01-a642-fe5a3030248e"); got != "claudia-377bf9c3" {
		t.Fatalf("SessionWindowName = %q, want claudia-377bf9c3", got)
	}
	if got := SessionWindowName("short"); got != "claudia-short" {
		t.Fatalf("SessionWindowName(short) = %q", got)
	}
	if got := SessionWindowName(""); got != "" {
		t.Fatalf("SessionWindowName(empty) = %q", got)
	}
}

// Mutant: exact Name()/SessionID only — the pre-T514 classifier. It
// must reap the live PO so this test is detecting the identity hole.
func TestT514MutationExactMatchOnlyKillsLivePO(t *testing.T) {
	p, names := t514LivePO()
	if exactMatchRegistered(p, names) {
		t.Fatal("exact-match mutant already keeps the live PO — this test is not detecting the hole")
	}
	if !registered(p, names) {
		t.Fatal("shipped registered() does not keep the live PO")
	}
}

func exactMatchRegistered(p Pane, names map[string]bool) bool {
	if names == nil {
		return false
	}
	if names[p.Name()] {
		return true
	}
	id := strings.TrimSpace(p.SessionID)
	return id != "" && names[id]
}

func TestT459ParseListPanes(t *testing.T) {
	raw := "claudia-anchor\tjv-a\t%3\t1234\tesc to interrupt\tjv-a\tsess-1\n" +
		"claudia-anchor\torphan\t%4\t5678\t\t\t\n"
	got := ParseListPanes(raw)
	if len(got) != 2 {
		t.Fatalf("parsed %d panes, want 2", len(got))
	}
	if got[0].AgentName != "jv-a" || got[0].ID != "%3" || InferFlight(got[0].Title) != FlightInFlight {
		t.Errorf("pane 0 = %+v", got[0])
	}
	if got[1].Window != "orphan" || InferFlight(got[1].Title) != FlightIdle {
		t.Errorf("pane 1 = %+v", got[1])
	}
}
