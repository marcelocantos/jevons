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
    depends_on: [T1]
    acceptance:
      - First criterion must hold
      - Second criterion must hold
    context: Fixture context for T181 hover card.
    tags: [ui, frontier]
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
