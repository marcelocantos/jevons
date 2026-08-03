// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestParseBullseyeLedgerPath(t *testing.T) {
	out := "# Startup context\nFile: /Users/me/work/jevons/bullseye.yaml\nActive: 3\n"
	got := parseBullseyeLedgerPath(out)
	if got != "/Users/me/work/jevons/bullseye.yaml" {
		t.Fatalf("got %q", got)
	}
	if parseBullseyeLedgerPath("no file here") != "" {
		t.Fatal("expected empty")
	}
}

func TestParseFrontierRows(t *testing.T) {
	out := `# Frontier
File: /tmp/proj/bullseye.yaml

🎯T27.1 The provider contract is specified  [Converging] — fanout=3
  tags: providers

🎯T131 RHS bottom pane frontier table  [Converging] — fanout=0

🎯T67 Composer enter-in-list  [Identified] -- fanout=1

19 target(s) ready for work
`
	rows := parseFrontierRows(out)
	if len(rows) != 3 {
		t.Fatalf("len=%d rows=%+v", len(rows), rows)
	}
	if rows[0].ID != "T27.1" || rows[0].Fanout != 3 || rows[0].Status != "Converging" {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[2].ID != "T67" || rows[2].Fanout != 1 {
		t.Fatalf("row2=%+v", rows[2])
	}
}

func TestComputeFrontierFromLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bullseye.yaml")
	// T1 done; T2 active depends on T1 → frontier; T3 active depends on T2 → blocked;
	// T2 blocks T3 so fanout(T2)=1. Dependents include id+name (🎯T179).
	yaml := `
schema_version: 5
targets:
  T1:
    name: Done base
    status: achieved
  T2:
    name: Ready leaf
    status: converging
    value: 5
    cost: 3
    depends_on: [T1]
    acceptance:
      - First criterion must hold
      - Second criterion must hold
    context: Fixture context for T181 hover card.
    tags: [ui, frontier]
    origin: owner
    discovered: "2026-08-03"
    owner: jevons-po
    custom_note: special extra field
  T3:
    name: Blocked
    status: identified
    depends_on: [T2]
  T4:
    name: Also ready
    status: identified
    value: 1
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := computeFrontierFromLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 frontier rows, got %d %+v", len(rows), rows)
	}
	// T2 has fanout 1, T4 fanout 0 → T2 first.
	if rows[0].ID != "T2" || rows[0].Fanout != 1 || rows[0].Name != "Ready leaf" {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[0].Status != "Converging" {
		t.Fatalf("status display=%q", rows[0].Status)
	}
	// 🎯T179: Dependents are active targets that depends_on this id; Fanout == len.
	if len(rows[0].Dependents) != 1 || rows[0].Fanout != len(rows[0].Dependents) {
		t.Fatalf("T2 dependents=%+v fanout=%d", rows[0].Dependents, rows[0].Fanout)
	}
	if rows[0].Dependents[0].ID != "T3" || rows[0].Dependents[0].Name != "Blocked" {
		t.Fatalf("T2 dep0=%+v", rows[0].Dependents[0])
	}
	// 🎯T181: acceptance/context/tags on list payload for rich hover cards.
	if len(rows[0].Acceptance) != 2 || rows[0].Acceptance[0] != "First criterion must hold" {
		t.Fatalf("T2 acceptance=%v", rows[0].Acceptance)
	}
	if rows[0].Context != "Fixture context for T181 hover card." {
		t.Fatalf("T2 context=%q", rows[0].Context)
	}
	if len(rows[0].Tags) != 2 || rows[0].Tags[0] != "ui" {
		t.Fatalf("T2 tags=%v", rows[0].Tags)
	}
	// 🎯T184: depends_on (outgoing) with names; cost; origin; extra unknown keys.
	if len(rows[0].DependsOn) != 1 || rows[0].DependsOn[0].ID != "T1" {
		t.Fatalf("T2 depends_on=%+v", rows[0].DependsOn)
	}
	if rows[0].DependsOn[0].Name != "Done base" {
		t.Fatalf("T2 depends_on name=%q", rows[0].DependsOn[0].Name)
	}
	if rows[0].Cost != 3 {
		t.Fatalf("T2 cost=%v", rows[0].Cost)
	}
	if rows[0].Origin != "owner" || rows[0].Discovered != "2026-08-03" {
		t.Fatalf("T2 origin/discovered=%q/%q", rows[0].Origin, rows[0].Discovered)
	}
	if rows[0].Extra == nil || rows[0].Extra["owner"] != "jevons-po" {
		t.Fatalf("T2 extra owner=%v", rows[0].Extra)
	}
	if rows[0].Extra["custom_note"] != "special extra field" {
		t.Fatalf("T2 extra custom_note=%v", rows[0].Extra)
	}
	if rows[1].ID != "T4" || rows[1].Fanout != 0 {
		t.Fatalf("row1=%+v", rows[1])
	}
	if len(rows[1].Dependents) != 0 {
		t.Fatalf("T4 should have empty dependents, got %+v", rows[1].Dependents)
	}
	// T3 must not appear (blocked by active T2).
	for _, r := range rows {
		if r.ID == "T3" {
			t.Fatal("blocked T3 must not be on frontier")
		}
	}
}

// 🎯T179: multi-dependent reverse edges carry id+name; Fanout matches len(Dependents).
func TestComputeFrontierDependentsList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bullseye.yaml")
	yaml := `
targets:
  T10.2:
    name: Ready parent
    status: converging
  T10.3:
    name: Client requests table drives server actions
    status: identified
    depends_on: [T10.2]
  T10.4:
    name: Reconnect uses diff sync only
    status: converging
    depends_on: [T10.2]
  T10.5:
    name: Done sibling
    status: achieved
    depends_on: [T10.2]
  T10.6:
    name: Also blocked
    status: identified
    depends_on: [T10.2]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := computeFrontierFromLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "T10.2" {
		t.Fatalf("want only T10.2 on frontier, got %+v", rows)
	}
	// Active dependents only (T10.5 achieved excluded).
	if rows[0].Fanout != 3 || len(rows[0].Dependents) != 3 {
		t.Fatalf("fanout=%d deps=%+v", rows[0].Fanout, rows[0].Dependents)
	}
	want := map[string]string{
		"T10.3": "Client requests table drives server actions",
		"T10.4": "Reconnect uses diff sync only",
		"T10.6": "Also blocked",
	}
	for _, d := range rows[0].Dependents {
		name, ok := want[d.ID]
		if !ok {
			t.Fatalf("unexpected dependent %+v", d)
		}
		if d.Name != name {
			t.Fatalf("dep %s name=%q want %q", d.ID, d.Name, name)
		}
		delete(want, d.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing dependents %v", want)
	}
	// Sorted by id.
	if rows[0].Dependents[0].ID != "T10.3" || rows[0].Dependents[1].ID != "T10.4" || rows[0].Dependents[2].ID != "T10.6" {
		t.Fatalf("order=%+v", rows[0].Dependents)
	}
}

func TestLoadFrontierUsesBullseyeDiscoveryNotHardcodedPath(t *testing.T) {
	prev := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prev })

	dir := t.TempDir()
	// Ledger lives in a shadow-like path — not under cwd — so hard-code would fail.
	shadow := filepath.Join(dir, "shadow", "bullseye.yaml")
	if err := os.MkdirAll(filepath.Dir(shadow), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
targets:
  T9:
    name: Live table
    status: converging
    value: 2
  T10:
    name: Blocked child
    status: identified
    depends_on: [T9]
`
	if err := os.WriteFile(shadow, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var saw [][]string
	runBullseyeCLI = func(args ...string) (string, error) {
		cp := append([]string{}, args...)
		saw = append(saw, cp)
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "open") {
			return "File: " + shadow + "\nActive: 1\n", nil
		}
		return "", nil
	}

	cwd := filepath.Join(dir, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	resp := loadFrontier(cwd)
	if !resp.Available {
		t.Fatalf("want available: %+v", resp)
	}
	if resp.Ledger != shadow {
		t.Fatalf("ledger must come from bullseye open, got %q want %q", resp.Ledger, shadow)
	}
	hard := filepath.Join(cwd, "bullseye.yaml")
	if resp.Ledger == hard {
		t.Fatal("must not hard-code cwd/bullseye.yaml")
	}
	if len(resp.Targets) != 1 || resp.Targets[0].ID != "T9" || resp.Targets[0].Fanout != 1 {
		t.Fatalf("targets=%+v", resp.Targets)
	}
	if len(saw) < 1 || !strings.Contains(strings.Join(saw[0], " "), "open") {
		t.Fatalf("expected bullseye open, saw %v", saw)
	}
	for _, args := range saw {
		if !strings.Contains(strings.Join(args, " "), "--cwd") {
			t.Fatalf("bullseye call missing --cwd: %v", args)
		}
	}
}

func TestLoadFrontierUnavailableWhenNotInitialized(t *testing.T) {
	prev := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prev })
	runBullseyeCLI = func(args ...string) (string, error) {
		return "code=not_initialized\nmessage: no bullseye.yaml found for /tmp\n", nil
	}
	resp := loadFrontier("/tmp/no-ledger")
	if resp.Available {
		t.Fatalf("want unavailable: %+v", resp)
	}
	if resp.Error == "" {
		t.Fatal("want calm error")
	}
	if len(resp.Targets) != 0 {
		t.Fatalf("targets=%v", resp.Targets)
	}
}

func TestHandleFrontierHTTP(t *testing.T) {
	prev := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prev })

	dir := t.TempDir()
	ledger := filepath.Join(dir, "bullseye.yaml")
	if err := os.WriteFile(ledger, []byte(`
targets:
  T1:
    name: Name
    status: converging
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runBullseyeCLI = func(args ...string) (string, error) {
		return "File: " + ledger + "\n", nil
	}
	s := New("test", t.TempDir())
	s.SetFrontierCwd(dir)
	req := httptest.NewRequest(http.MethodGet, "/api/frontier", nil)
	rr := httptest.NewRecorder()
	s.handleFrontier(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp FrontierResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Available || len(resp.Targets) != 1 || resp.Targets[0].ID != "T1" {
		t.Fatalf("resp=%+v", resp)
	}
}

// 🎯T185: unachieved graph from ledger — active nodes + depends_on edges among them.
// 🎯T190: multi-diagram pack (component + orphans), not one mega subgraph strip.
func TestComputeActiveGraphMermaidFromLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bullseye.yaml")
	yaml := `
schema_version: 5
targets:
  T1:
    name: Done base
    status: achieved
  T2:
    name: Ready leaf
    status: converging
    depends_on: [T1]
  T3:
    name: Blocked child
    status: identified
    depends_on: [T2]
  T3.1:
    name: Nested active
    status: converging
    depends_on: [T3, T1]
  T4:
    name: Orphan active
    status: identified
  T5:
    name: Parked
    status: set_aside
    depends_on: [T2]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	pack, err := computeActiveGraphPackFromLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	// Active: T2, T3, T3.1, T4 (not T1 achieved, not T5 set_aside).
	if pack.NodeCount != 4 {
		t.Fatalf("nodes=%d want 4; pack=%+v", pack.NodeCount, pack)
	}
	// Edges among active only: T3→T2, T3.1→T3 (T3.1→T1 dropped; T2→T1 dropped).
	if pack.EdgeCount != 2 {
		t.Fatalf("edges=%d want 2", pack.EdgeCount)
	}
	if pack.Pack != frontierGraphPackLayout {
		t.Fatalf("pack=%q want %q", pack.Pack, frontierGraphPackLayout)
	}
	// Multi-diagram: one component {T2,T3,T3.1} + one orphans {T4}.
	if len(pack.Diagrams) != 2 {
		t.Fatalf("diagrams=%d want 2: %+v", len(pack.Diagrams), pack.Diagrams)
	}
	var comp, orph *GraphDiagramBlock
	for i := range pack.Diagrams {
		d := &pack.Diagrams[i]
		switch d.Kind {
		case graphDiagramKindComponent:
			comp = d
		case graphDiagramKindOrphans:
			orph = d
		}
	}
	if comp == nil || orph == nil {
		t.Fatalf("want component+orphans: %+v", pack.Diagrams)
	}
	if comp.NodeCount != 3 || comp.EdgeCount != 2 {
		t.Fatalf("component nodes=%d edges=%d:\n%s", comp.NodeCount, comp.EdgeCount, comp.Mermaid)
	}
	if orph.NodeCount != 1 || orph.EdgeCount != 0 {
		t.Fatalf("orphans nodes=%d edges=%d:\n%s", orph.NodeCount, orph.EdgeCount, orph.Mermaid)
	}
	// Layout directives inside each diagram (not Mermaid-native TD alone as sole fix).
	for _, d := range pack.Diagrams {
		if !strings.Contains(d.Mermaid, "%%{init:") {
			t.Fatalf("missing mermaid init in %s:\n%s", d.ID, d.Mermaid)
		}
		if !strings.Contains(d.Mermaid, "useMaxWidth") || !strings.Contains(d.Mermaid, "nodeSpacing") ||
			!strings.Contains(d.Mermaid, "rankSpacing") || !strings.Contains(d.Mermaid, "wrappingWidth") {
			t.Fatalf("missing layout knobs in %s:\n%s", d.ID, d.Mermaid)
		}
		if !strings.Contains(d.Mermaid, "flowchart TB") {
			t.Fatalf("missing flowchart TB in %s:\n%s", d.ID, d.Mermaid)
		}
		// Reject single-mega subgraph packing as the product shape.
		if strings.Contains(d.Mermaid, "subgraph island_") {
			t.Fatalf("diagrams must not use subgraph island packing:\n%s", d.Mermaid)
		}
	}
	if !strings.Contains(comp.Mermaid, "T2[") || !strings.Contains(comp.Mermaid, "T3[") || !strings.Contains(comp.Mermaid, "T3_1[") {
		t.Fatalf("missing component nodes:\n%s", comp.Mermaid)
	}
	if !strings.Contains(orph.Mermaid, "T4[") {
		t.Fatalf("missing orphan node:\n%s", orph.Mermaid)
	}
	if strings.Contains(comp.Mermaid, "T1[") || strings.Contains(comp.Mermaid, "T5[") ||
		strings.Contains(orph.Mermaid, "T1[") || strings.Contains(orph.Mermaid, "T5[") {
		t.Fatalf("achieved/set_aside must not appear")
	}
	if !strings.Contains(comp.Mermaid, "T3 -.->|needs| T2") {
		t.Fatalf("missing T3→T2 edge:\n%s", comp.Mermaid)
	}
	if !strings.Contains(comp.Mermaid, "T3_1 -.->|needs| T3") {
		t.Fatalf("missing T3.1→T3 edge:\n%s", comp.Mermaid)
	}
	if strings.Contains(comp.Mermaid, "|needs| T1") {
		t.Fatalf("edge to achieved T1 must be omitted:\n%s", comp.Mermaid)
	}
	// Joined pin source carries multi-diagram packing directives.
	if !strings.Contains(pack.Mermaid, "jevons-frontier-pack") || !strings.Contains(pack.Mermaid, "pack=wrap-grid") {
		t.Fatalf("joined source missing pack markers:\n%s", pack.Mermaid)
	}
	if !strings.Contains(pack.Mermaid, "kind=component") || !strings.Contains(pack.Mermaid, "kind=orphans") {
		t.Fatalf("joined source missing diagram kinds:\n%s", pack.Mermaid)
	}
	// Legacy single-string entry still returns joined multi-diagram source.
	src, nodes, edges, err := computeActiveGraphMermaidFromLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if nodes != 4 || edges != 2 {
		t.Fatalf("legacy nodes=%d edges=%d", nodes, edges)
	}
	if !strings.Contains(src, "jevons-frontier-pack") {
		t.Fatalf("legacy mermaid missing pack:\n%s", src)
	}
}

func TestSplitOrphanComponents(t *testing.T) {
	connected, orphans := splitOrphanComponents([][]string{
		{"A", "B"},
		{"C"},
		{"D", "E", "F"},
		{"G"},
	})
	if len(connected) != 2 {
		t.Fatalf("connected=%v", connected)
	}
	if strings.Join(orphans, ",") != "C,G" {
		t.Fatalf("orphans=%v", orphans)
	}
}

func TestPackActiveGraphDiagramsOrder(t *testing.T) {
	// Larger edge-count component first; orphans last.
	connected := [][]string{
		{"A", "B"},          // 1 edge
		{"D", "E", "F", "G"}, // 3 edges
	}
	orphans := []string{"Z", "Y"}
	nameOf := map[string]string{}
	edges := []rawGraphEdge{
		{from: "B", to: "A"},
		{from: "E", to: "D"},
		{from: "F", to: "E"},
		{from: "G", to: "F"},
	}
	blocks := packActiveGraphDiagrams(connected, orphans, nameOf, edges)
	if len(blocks) != 3 {
		t.Fatalf("blocks=%d %+v", len(blocks), blocks)
	}
	if blocks[0].Kind != graphDiagramKindComponent || blocks[0].NodeCount != 4 {
		t.Fatalf("first should be largest component: %+v", blocks[0])
	}
	if blocks[1].NodeCount != 2 {
		t.Fatalf("second component: %+v", blocks[1])
	}
	if blocks[2].Kind != graphDiagramKindOrphans || blocks[2].NodeCount != 2 {
		t.Fatalf("orphans last: %+v", blocks[2])
	}
}

func TestPackIslandsFromAdj(t *testing.T) {
	// A-B connected, C alone, D-E connected.
	active := []string{"B", "A", "E", "C", "D"}
	adj := map[string][]string{
		"A": {"B"},
		"B": {"A"},
		"C": nil,
		"D": {"E"},
		"E": {"D"},
	}
	// Discovery order follows active slice; components sorted by first id.
	islands := packIslandsFromAdj(active, adj)
	if len(islands) != 3 {
		t.Fatalf("islands=%v", islands)
	}
	if got := strings.Join(islands[0], ","); got != "A,B" {
		t.Fatalf("island0=%v", islands[0])
	}
	if got := strings.Join(islands[1], ","); got != "C" {
		t.Fatalf("island1=%v", islands[1])
	}
	if got := strings.Join(islands[2], ","); got != "D,E" {
		t.Fatalf("island2=%v", islands[2])
	}
}

// 🎯T199: natural/version order for target ids (not pure lex).
func TestTargetIDLessNatural(t *testing.T) {
	// Canonical acceptance chain from bullseye T199.
	want := []string{"T1", "T2", "T10", "T10.2", "T27", "T27.3", "T100"}
	got := append([]string(nil), want...)
	// Shuffle into lex-wrong order (T10 before T2 under pure string sort).
	got = []string{"T100", "T10", "T27.3", "T2", "T10.2", "T1", "T27"}
	sort.Slice(got, func(i, j int) bool { return targetIDLess(got[i], got[j]) })
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("natural sort:\n got %v\nwant %v", got, want)
	}

	// Sub-versions: T10.2 < T10.10 (lex would reverse).
	if !targetIDLess("T10.2", "T10.10") {
		t.Fatal("expected T10.2 < T10.10")
	}
	if targetIDLess("T10.10", "T10.2") {
		t.Fatal("expected not T10.10 < T10.2")
	}

	// Child after parent, before next sibling root.
	if !targetIDLess("T1", "T1.1") {
		t.Fatal("T1 < T1.1")
	}
	if !targetIDLess("T1.1", "T2") {
		t.Fatal("T1.1 < T2")
	}

	// Equal ids are not less.
	if targetIDLess("T10", "T10") {
		t.Fatal("equal should not be less")
	}

	// Residual: non-T prefixes still segment-split digit runs.
	if !targetIDLess("foo2", "foo10") {
		t.Fatal("foo2 < foo10")
	}
	if targetIDLess("foo10", "foo2") {
		t.Fatal("not foo10 < foo2")
	}

	// Leading zeros: same magnitude, shorter raw run first.
	if !targetIDLess("T1", "T01") {
		t.Fatal("T1 < T01 (equal magnitude, fewer digits)")
	}
}

func TestPackIslandsNaturalTargetIDs(t *testing.T) {
	// Island packing must use natural id order (🎯T199).
	active := []string{"T10", "T2", "T100", "T10.2"}
	adj := map[string][]string{
		"T2":    {"T10"},
		"T10":   {"T2"},
		"T10.2": nil,
		"T100":  nil,
	}
	islands := packIslandsFromAdj(active, adj)
	// Islands ordered by first id: {T2,T10}, {T10.2}, {T100}
	if len(islands) != 3 {
		t.Fatalf("islands=%v", islands)
	}
	if got := strings.Join(islands[0], ","); got != "T2,T10" {
		t.Fatalf("island0 natural=%v (lex would put T10 first)", islands[0])
	}
	if got := strings.Join(islands[1], ","); got != "T10.2" {
		t.Fatalf("island1=%v", islands[1])
	}
	if got := strings.Join(islands[2], ","); got != "T100" {
		t.Fatalf("island2=%v", islands[2])
	}
}

func TestMermaidActiveGraphHeader(t *testing.T) {
	h := mermaidActiveGraphHeader()
	if !strings.HasPrefix(h, "%%{init:") {
		t.Fatalf("prefix: %q", h)
	}
	if !strings.Contains(h, "flowchart TB") {
		t.Fatalf("missing flowchart TB: %q", h)
	}
	for _, tok := range []string{"useMaxWidth", "nodeSpacing", "rankSpacing", "wrappingWidth"} {
		if !strings.Contains(h, tok) {
			t.Fatalf("missing %s: %q", tok, h)
		}
	}
}

func TestMermaidSafeNodeID(t *testing.T) {
	if got := mermaidSafeNodeID("T27.1"); got != "T27_1" {
		t.Fatalf("got %q", got)
	}
	if got := mermaidSafeNodeID("9bad"); got != "n_9bad" {
		t.Fatalf("got %q", got)
	}
}

func TestHandleFrontierGraphHTTP(t *testing.T) {
	prev := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prev })

	dir := t.TempDir()
	ledger := filepath.Join(dir, "bullseye.yaml")
	if err := os.WriteFile(ledger, []byte(`
targets:
  T10:
    name: Base
    status: converging
  T11:
    name: Child
    status: identified
    depends_on: [T10]
  T99:
    name: Done
    status: achieved
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runBullseyeCLI = func(args ...string) (string, error) {
		return "File: " + ledger + "\n", nil
	}
	s := New("test", t.TempDir())
	s.SetFrontierCwd(dir)
	req := httptest.NewRequest(http.MethodGet, "/api/frontier/graph", nil)
	rr := httptest.NewRecorder()
	s.handleFrontierGraph(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp GraphResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Available {
		t.Fatalf("want available: %+v", resp)
	}
	if resp.NodeCount != 2 || resp.EdgeCount != 1 {
		t.Fatalf("nodes=%d edges=%d mermaid=%s", resp.NodeCount, resp.EdgeCount, resp.Mermaid)
	}
	if resp.Pack != frontierGraphPackLayout {
		t.Fatalf("pack=%q", resp.Pack)
	}
	if len(resp.Diagrams) != 1 {
		// Single connected component {T10,T11}, no orphans.
		t.Fatalf("diagrams=%d want 1: %+v", len(resp.Diagrams), resp.Diagrams)
	}
	if resp.Diagrams[0].Kind != graphDiagramKindComponent {
		t.Fatalf("kind=%s", resp.Diagrams[0].Kind)
	}
	if !strings.Contains(resp.Diagrams[0].Mermaid, "T11 -.->|needs| T10") {
		t.Fatalf("diagram mermaid=%s", resp.Diagrams[0].Mermaid)
	}
	if !strings.Contains(resp.Mermaid, "T11 -.->|needs| T10") {
		t.Fatalf("joined mermaid=%s", resp.Mermaid)
	}
	if strings.Contains(resp.Mermaid, "T99") {
		t.Fatalf("achieved T99 leaked: %s", resp.Mermaid)
	}
}

func TestStripMermaidFenceCLI(t *testing.T) {
	in := "```mermaid\ngraph TD\n  A --> B\n```"
	got := stripMermaidFenceCLI(in)
	if got != "graph TD\n  A --> B" {
		t.Fatalf("got %q", got)
	}
	if stripMermaidFenceCLI("graph TD") != "graph TD" {
		t.Fatal("raw passthrough")
	}
}

func TestNotifyFrontierChangedShape(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 1)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	s.NotifyFrontierChanged()
	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "agents_changed" && m["type"] != "frontier_changed" {
			// exact type
		}
		if m["type"] != "frontier_changed" {
			t.Fatalf("type=%v", m["type"])
		}
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
}

func TestIsBullseyeNotInitialized(t *testing.T) {
	if !isBullseyeNotInitialized("code=not_initialized\nmessage: no bullseye.yaml found") {
		t.Fatal("expected true")
	}
	if isBullseyeNotInitialized("File: /a/bullseye.yaml") {
		t.Fatal("expected false")
	}
}

// Live smoke against real bullseye CLI + this repo ledger (skipped if no CLI).
func TestLoadFrontierLiveJevonsRepo(t *testing.T) {
	if _, err := exec.LookPath("bullseye"); err != nil {
		t.Skip("bullseye not on PATH")
	}
	prev := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prev })
	runBullseyeCLI = func(args ...string) (string, error) {
		cmd := exec.Command("bullseye", args...)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		return buf.String(), err
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/server → repo root
	root := filepath.Clean(filepath.Join(cwd, "../.."))
	resp := loadFrontier(root)
	if !resp.Available {
		t.Fatalf("live frontier unavailable: %+v", resp)
	}
	if resp.Ledger == "" || !strings.Contains(resp.Ledger, "bullseye.yaml") {
		t.Fatalf("ledger path=%q", resp.Ledger)
	}
	if !filepath.IsAbs(resp.Ledger) {
		t.Fatalf("want abs ledger, got %q", resp.Ledger)
	}
	if len(resp.Targets) == 0 {
		t.Fatal("expected some frontier rows from live ledger")
	}
	t.Logf("live frontier: ledger=%s n=%d", resp.Ledger, len(resp.Targets))
}
