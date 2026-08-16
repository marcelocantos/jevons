// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpscope

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	jevonsDir  = "/Users/marcelo/work/github.com/marcelocantos/jevons"
	claudiaDir = "/Users/marcelo/work/github.com/marcelocantos/claudia"
)

// incidentConfig is the state of ~/.claude.json on 2026-08-15: jevonsmcp
// registered in local scope under the jevons repo path and nowhere else,
// alongside the long tail of unrelated client state the file also carries.
func incidentConfig(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"numStartups": 1234,
		"theme":       "dark",
		"anonymousId": "abc-123",
		"tipsHistory": map[string]any{"shift-enter": 7},
		"mcpServers": map[string]any{
			"bullseye": map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
			"sawmill":  map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
		},
		"projects": map[string]any{
			jevonsDir: map[string]any{
				"allowedTools": []string{},
				"mcpServers": map[string]any{
					ServerName: map[string]any{"type": "http", "url": DefaultEndpoint},
				},
			},
			claudiaDir: map[string]any{
				"allowedTools": []string{},
			},
			"/Users/marcelo/work/github.com/minicadesmobile/stock-car-racing": map[string]any{
				"mcpServers": map[string]any{
					"UnityMCP": map[string]any{"type": "http", "url": "http://127.0.0.1:9999/mcp"},
				},
			},
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return data
}

// liveEndpoint returns a URL that something is really listening on, and the
// listener is closed when the test ends.
func liveEndpoint(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return "http://" + ln.Addr().String() + "/mcp"
}

// deadEndpoint returns a URL whose port was bound and then released, so a
// dial is refused rather than filtered — the state a genuinely dead daemon
// leaves behind.
func deadEndpoint(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + addr + "/mcp"
}

func writeConfig(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestT464OutOfScopeIsNotDown replays the misdiagnosis that cost the fleet a
// cycle: an agent outside the jevons repo has no fleet tools, and the daemon
// is answering the whole time. The verdict must name the registration, must
// state the control plane is up, and must not entitle anyone to report an
// outage.
//
// Its own control is the second half: the identical config against a refused
// endpoint DOES yield an outage claim, so a test that passed by never
// claiming anything would fail here.
func TestT464OutOfScopeIsNotDown(t *testing.T) {
	cfg := writeConfig(t, incidentConfig(t))

	up := DiagnoseSession(cfg, ServerName, claudiaDir, liveEndpoint(t))
	if up.Verdict != VerdictOutOfScope {
		t.Fatalf("daemon up, registration out of scope: got verdict %q (scope %q, reach %q), want %q",
			up.Verdict, up.Scope, up.Reach, VerdictOutOfScope)
	}
	if up.Verdict.ClaimsControlPlaneDown() {
		t.Fatal("out-of-scope verdict claims a control-plane outage; that is the 🎯T464 misdiagnosis")
	}
	if !strings.Contains(up.Message, "NOT REGISTERED FOR THIS WORKING DIRECTORY") {
		t.Errorf("message does not name the real fault:\n%s", up.Message)
	}
	if !strings.Contains(up.Message, claudiaDir) {
		t.Errorf("message does not name the working directory %s:\n%s", claudiaDir, up.Message)
	}
	if !strings.Contains(up.Message, "CONTROL PLANE IS UP") {
		t.Errorf("message does not state that the daemon is up, which is the whole point:\n%s", up.Message)
	}
	if strings.Contains(strings.ToLower(up.Message), "control plane is down") {
		t.Errorf("message reports an outage that is not happening:\n%s", up.Message)
	}
	if len(up.LocalDirs) != 1 || up.LocalDirs[0] != jevonsDir {
		t.Errorf("LocalDirs = %v, want exactly [%s] so the reader can see the shape of the misconfiguration",
			up.LocalDirs, jevonsDir)
	}

	// Control: same config, nothing listening. The outage claim must now
	// appear, or the assertions above prove nothing.
	down := DiagnoseSession(cfg, ServerName, claudiaDir, deadEndpoint(t))
	if down.Verdict != VerdictUnregisteredAndDown {
		t.Fatalf("control: daemon down and out of scope: got %q (reach %q), want %q",
			down.Verdict, down.Reach, VerdictUnregisteredAndDown)
	}
	if !down.Verdict.ClaimsControlPlaneDown() {
		t.Fatalf("control: a refused endpoint must license an outage report, else the test above is vacuous")
	}
}

// TestT464RegisteredAndDownStillReportsDown keeps the fix from swinging the
// other way: suppressing every outage report would pass the test above and
// would be worse than the bug.
func TestT464RegisteredAndDownStillReportsDown(t *testing.T) {
	cfg := writeConfig(t, incidentConfig(t))
	d := DiagnoseSession(cfg, ServerName, jevonsDir, deadEndpoint(t))
	if d.Verdict != VerdictDown {
		t.Fatalf("registered here and nothing listening: got %q (scope %q, reach %q), want %q",
			d.Verdict, d.Scope, d.Reach, VerdictDown)
	}
	if !d.Verdict.ClaimsControlPlaneDown() {
		t.Fatalf("a real outage must be reportable as one")
	}
	if !strings.Contains(d.Message, "DOWN") {
		t.Errorf("message does not report the outage:\n%s", d.Message)
	}
}

// TestT464UnknownReachNeverClaimsAnOutage pins the mapping that, if widened,
// reintroduces the false report: a dial that proves nothing is not evidence
// of a dead daemon under any registration state.
func TestT464UnknownReachNeverClaimsAnOutage(t *testing.T) {
	for _, scope := range []Scope{ScopeNone, ScopeUser, ScopeLocal} {
		v := Diagnose(scope, ReachUnknown)
		if v.ClaimsControlPlaneDown() {
			t.Errorf("Diagnose(%q, unknown) = %q, which claims an outage on a probe that settled nothing", scope, v)
		}
		if v != VerdictUnknown {
			t.Errorf("Diagnose(%q, unknown) = %q, want %q", scope, v, VerdictUnknown)
		}
	}
}

// TestT464UserScopeReachesEveryWorkingDirectory is acceptance clause 1 in
// pure form: once the registration is in user scope, no directory is
// privileged.
func TestT464UserScopeReachesEveryWorkingDirectory(t *testing.T) {
	fixed, changed, err := EnsureUserScope(incidentConfig(t), ServerName, HTTPEntry(DefaultEndpoint))
	if err != nil {
		t.Fatalf("EnsureUserScope: %v", err)
	}
	if !changed {
		t.Fatal("the incident config has no user-scope entry, so ensure must report a change")
	}
	for _, cwd := range []string{claudiaDir, jevonsDir, "/Users/marcelo/.jevons/jevons", "/tmp", "/"} {
		scope, entry, err := FindScope(fixed, ServerName, cwd)
		if err != nil {
			t.Fatalf("FindScope(%s): %v", cwd, err)
		}
		if scope != ScopeUser {
			t.Errorf("FindScope(%s) = %q, want %q — fleet control must follow the agent, not the directory", cwd, scope, ScopeUser)
		}
		if entry.URL != DefaultEndpoint {
			t.Errorf("FindScope(%s) URL = %q, want %q", cwd, entry.URL, DefaultEndpoint)
		}
		if v := Diagnose(scope, ReachUp); v != VerdictOK {
			t.Errorf("Diagnose after fix from %s = %q, want %q", cwd, v, VerdictOK)
		}
	}
}

// TestT464LocalScopeIsExactMatch pins the reading of the projects map. A
// subdirectory gets its own key and inherits nothing, so reporting fleet
// control for one would be a promise the session cannot keep.
func TestT464LocalScopeIsExactMatch(t *testing.T) {
	cfg := incidentConfig(t)
	cases := []struct {
		cwd  string
		want Scope
	}{
		{jevonsDir, ScopeLocal},
		{jevonsDir + "/internal", ScopeNone},
		{claudiaDir, ScopeNone},
		{"/Users/marcelo/.jevons/jevons", ScopeNone},
	}
	for _, c := range cases {
		got, _, err := FindScope(cfg, ServerName, c.cwd)
		if err != nil {
			t.Fatalf("FindScope(%s): %v", c.cwd, err)
		}
		if got != c.want {
			t.Errorf("FindScope(%s) = %q, want %q", c.cwd, got, c.want)
		}
	}
}

// TestT464EnsurePreservesEverythingElse is the 🎯T376 property in pure form:
// the file carries 1800 project entries and a long tail of client state this
// product has never modelled, and a repair that dropped any of it would be a
// worse outage than the one it fixes.
func TestT464EnsurePreservesEverythingElse(t *testing.T) {
	before := incidentConfig(t)
	after, changed, err := EnsureUserScope(before, ServerName, HTTPEntry(DefaultEndpoint))
	if err != nil {
		t.Fatalf("EnsureUserScope: %v", err)
	}
	if !changed {
		t.Fatal("want changed")
	}

	var was, now map[string]any
	if err := json.Unmarshal(before, &was); err != nil {
		t.Fatalf("decode before: %v", err)
	}
	if err := json.Unmarshal(after, &now); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	for key, want := range was {
		got, ok := now[key]
		if !ok {
			t.Fatalf("ensure dropped top-level key %q", key)
		}
		if key == "mcpServers" {
			continue // the one key it is allowed to change
		}
		if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
			t.Errorf("ensure altered unrelated key %q:\n got %#v\nwant %#v", key, got, want)
		}
	}
	// The local-scope entry is left alone: removing it is a separate
	// decision, and a repair that also deletes is harder to trust.
	if scope, _, err := FindScope(after, ServerName, jevonsDir); err != nil || scope != ScopeUser {
		t.Errorf("after ensure, FindScope(jevons) = %q (err %v); user scope must now win", scope, err)
	}
	servers := now["mcpServers"].(map[string]any)
	for _, keep := range []string{"bullseye", "sawmill"} {
		if _, ok := servers[keep]; !ok {
			t.Errorf("ensure dropped another tool's user-scope registration %q", keep)
		}
	}
}

// TestT464EnsureIsANoOpWhenAlreadyCorrect matters because the file is hot:
// a repair that rewrote it on every call would take the lock, and the risk
// that comes with it, for nothing.
func TestT464EnsureIsANoOpWhenAlreadyCorrect(t *testing.T) {
	fixed, _, err := EnsureUserScope(incidentConfig(t), ServerName, HTTPEntry(DefaultEndpoint))
	if err != nil {
		t.Fatalf("EnsureUserScope: %v", err)
	}
	again, changed, err := EnsureUserScope(fixed, ServerName, HTTPEntry(DefaultEndpoint))
	if err != nil {
		t.Fatalf("second EnsureUserScope: %v", err)
	}
	if changed {
		t.Fatal("ensure rewrote a config that was already correct")
	}
	if string(again) != string(fixed) {
		t.Fatal("ensure returned different bytes for an unchanged config")
	}

	path := writeConfig(t, fixed)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	wroteChange, err := WriteEnsure(path, ServerName, HTTPEntry(DefaultEndpoint))
	if err != nil {
		t.Fatalf("WriteEnsure: %v", err)
	}
	if wroteChange {
		t.Error("WriteEnsure reported a change to an already-correct config")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(info.ModTime()) {
		t.Error("WriteEnsure touched a file it had nothing to change")
	}
	if _, err := os.Stat(path + ".jevons-lock"); err == nil {
		t.Error("WriteEnsure took the lock on the hot file with nothing to write")
	}
}

// TestT464ConcurrentWritersDoNotLoseEachOther is the 🎯T376 failure mode as
// an executable check: several agents repairing the same file at once must
// end with every registration present, not with the last writer's view of a
// document it read before the others wrote.
func TestT464ConcurrentWritersDoNotLoseEachOther(t *testing.T) {
	path := writeConfig(t, incidentConfig(t))
	const writers = 12

	var wg sync.WaitGroup
	errs := make([]error, writers)
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = WriteEnsure(path, fmt.Sprintf("server-%02d", i), HTTPEntry(fmt.Sprintf("http://127.0.0.1:%d/mcp", 20000+i)))
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	for i := range writers {
		name := fmt.Sprintf("server-%02d", i)
		scope, _, err := FindScope(final, name, "/anywhere")
		if err != nil {
			t.Fatalf("FindScope(%s): %v", name, err)
		}
		if scope != ScopeUser {
			t.Errorf("%s was lost to a concurrent writer (scope %q) — the 🎯T376 failure mode", name, scope)
		}
	}
	// The registrations that were already there must have survived too.
	for _, name := range []string{"bullseye", "sawmill"} {
		if scope, _, _ := FindScope(final, name, "/anywhere"); scope != ScopeUser {
			t.Errorf("pre-existing registration %q was lost", name)
		}
	}
}

// TestT464WriteEnsureRepairsTheIncident is the write path end to end: the
// 2026-08-15 config goes in, and a session in any directory comes out with
// fleet control and an ok verdict.
func TestT464WriteEnsureRepairsTheIncident(t *testing.T) {
	path := writeConfig(t, incidentConfig(t))
	endpoint := liveEndpoint(t)

	before := DiagnoseSession(path, ServerName, claudiaDir, endpoint)
	if before.Verdict != VerdictOutOfScope {
		t.Fatalf("precondition: got %q, want %q", before.Verdict, VerdictOutOfScope)
	}

	changed, err := WriteEnsure(path, ServerName, HTTPEntry(endpoint))
	if err != nil {
		t.Fatalf("WriteEnsure: %v", err)
	}
	if !changed {
		t.Fatal("WriteEnsure reported no change to a config that needed one")
	}

	after := DiagnoseSession(path, ServerName, claudiaDir, endpoint)
	if after.Verdict != VerdictOK {
		t.Fatalf("after repair from %s: got %q (scope %q, reach %q), want %q",
			claudiaDir, after.Verdict, after.Scope, after.Reach, VerdictOK)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("repair changed the config mode to %v; the file holds credentials", info.Mode().Perm())
	}
}

// TestT464UnreadableConfigIsUndetermined keeps the third confusion out: a
// config that cannot be read is not a config with no registration, and the
// two lead to opposite actions.
func TestT464UnreadableConfigIsUndetermined(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	d := DiagnoseSession(missing, ServerName, claudiaDir, liveEndpoint(t))
	if d.Verdict != VerdictUnknown {
		t.Fatalf("missing config: got %q, want %q", d.Verdict, VerdictUnknown)
	}
	if d.Verdict.ClaimsControlPlaneDown() {
		t.Fatal("an unreadable config must not license an outage report")
	}

	garbage := writeConfig(t, []byte("{not json"))
	d = DiagnoseSession(garbage, ServerName, claudiaDir, liveEndpoint(t))
	if d.Verdict != VerdictUnknown {
		t.Fatalf("malformed config: got %q, want %q", d.Verdict, VerdictUnknown)
	}
	if _, _, err := FindScope([]byte("{not json"), ServerName, claudiaDir); err == nil {
		t.Fatal("FindScope silently reported no registration for a config it could not parse")
	}
}
