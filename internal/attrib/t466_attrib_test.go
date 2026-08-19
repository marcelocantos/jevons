// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package attrib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRepo builds a throwaway repo with one committed base file per name.
func gitRepo(t *testing.T, base map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		if out, err := Git(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustGit("init", "-q", "-b", "master")
	mustGit("config", "user.email", "t466@test")
	mustGit("config", "user.name", "t466")
	for name, content := range base {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustGit("add", "-A")
	mustGit("commit", "-q", "-m", "base")
	return dir
}

func write(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, repo, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestT466StoreRoundTripSkipsMalformed(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	at := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	if err := store.Append("s1", []Record{
		{Path: "a.go", Agent: "jv-a", Tool: "Write", At: at, Via: ViaHook},
	}); err != nil {
		t.Fatal(err)
	}
	// A torn line must cost one record, not the whole store.
	f := filepath.Join(store.Root, "sessions", "s1", "touches.jsonl")
	fh, err := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fh.WriteString(`{"path":"torn`)
	fh.Close()
	if err := store.Append("s1", []Record{
		{Path: "b.go", Agent: "jv-a", At: at.Add(time.Minute), Via: ViaHook},
	}); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 records surviving the torn line, got %d: %+v", len(records), records)
	}
	if records[0].Session != "s1" || records[0].Path != "a.go" {
		t.Fatalf("unexpected first record %+v", records[0])
	}
}

func TestT466AttributeSoleSharedUnattributed(t *testing.T) {
	at := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	records := []Record{
		{Session: "s1", Agent: "jv-a", Path: "only-a.go", At: at, Via: ViaHook},
		{Session: "s1", Agent: "jv-a", Path: "both.go", At: at, Via: ViaHook},
		{Session: "s2", Agent: "jv-b", Path: "both.go", At: at.Add(time.Minute), Via: ViaHook},
		// Committed since: not dirty, must not appear anywhere.
		{Session: "s2", Agent: "jv-b", Path: "committed.go", At: at, Via: ViaHook},
	}
	dirty := []DirtyPath{
		{Path: "only-a.go", XY: " M"},
		{Path: "both.go", XY: " M"},
		{Path: "nobody.go", XY: "??"},
	}
	att := Attribute(records, dirty)

	a, ok := att.Slice("jv-a")
	if !ok {
		t.Fatal("no slice for jv-a")
	}
	if got := a.Paths(false); len(got) != 1 || got[0] != "only-a.go" {
		t.Fatalf("jv-a sole = %v", got)
	}
	if got := a.Paths(true); len(got) != 2 {
		t.Fatalf("jv-a with shared = %v", got)
	}
	b, _ := att.Slice("jv-b")
	if got := b.Paths(false); len(got) != 0 {
		t.Fatalf("jv-b sole should be empty (both.go is contested), got %v", got)
	}
	if len(att.Unattributed) != 1 || att.Unattributed[0].Path != "nobody.go" {
		t.Fatalf("unattributed = %+v", att.Unattributed)
	}
	for _, s := range att.Slices {
		for _, p := range append(s.Sole, s.Shared...) {
			if p.Path == "committed.go" {
				t.Fatal("a committed path leaked into a slice")
			}
		}
	}
}

// TestT466DiscardOneAgentSliceAlone is the acceptance's core clause: recover
// or discard one stopped agent's slice without a bulk checkout that destroys
// the other N-1.
func TestT466DiscardOneAgentSliceAlone(t *testing.T) {
	repo := gitRepo(t, map[string]string{"a.go": "base a\n", "b.go": "base b\n"})
	// Agent A: modified a tracked file and left an untracked new one.
	write(t, repo, "a.go", "agent a's edit\n")
	write(t, repo, "new-a.go", "agent a's new file\n")
	// Agent B: modified a different tracked file.
	write(t, repo, "b.go", "agent b's edit\n")

	at := time.Now()
	records := []Record{
		{Session: "sa", Agent: "jv-a", Path: "a.go", At: at, Via: ViaHook},
		{Session: "sa", Agent: "jv-a", Path: "new-a.go", At: at, Via: ViaHook},
		{Session: "sb", Agent: "jv-b", Path: "b.go", At: at, Via: ViaHook},
	}
	dirty, err := DirtyPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	att := Attribute(records, dirty)
	slice, ok := att.Slice("jv-a")
	if !ok {
		t.Fatal("no slice for jv-a")
	}

	outRoot := t.TempDir()
	saved, err := Save(repo, outRoot, "jv-a", slice.Paths(false), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := Discard(repo, slice.Paths(false), saved); err != nil {
		t.Fatal(err)
	}

	if got := read(t, repo, "a.go"); got != "base a\n" {
		t.Fatalf("a.go not restored to HEAD: %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "new-a.go")); !os.IsNotExist(err) {
		t.Fatal("untracked new-a.go should be gone after discard")
	}
	// The other agent's work is untouched — the property the bulk checkout
	// destroys.
	if got := read(t, repo, "b.go"); got != "agent b's edit\n" {
		t.Fatalf("jv-b's edit was destroyed by jv-a's discard: %q", got)
	}

	// And the discard is reversible: restore brings the slice back.
	if err := Restore(repo, saved); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.go"); got != "agent a's edit\n" {
		t.Fatalf("a.go not restored from slice: %q", got)
	}
	if got := read(t, repo, "new-a.go"); got != "agent a's new file\n" {
		t.Fatalf("new-a.go not restored from slice: %q", got)
	}
}

func TestT466DiscardRefusesWithoutSave(t *testing.T) {
	repo := gitRepo(t, map[string]string{"a.go": "base\n"})
	write(t, repo, "a.go", "edit\n")
	if err := Discard(repo, []string{"a.go"}, nil); err == nil {
		t.Fatal("discard without a saved slice must refuse")
	}
}

// TestT466DrainOnStopLeavesIndexClean is the acceptance's second clause:
// `git diff --cached --name-only` is clean after any agent stops, with the
// staged state saved, attributed, and the working tree bytes untouched.
func TestT466DrainOnStopLeavesIndexClean(t *testing.T) {
	repo := gitRepo(t, map[string]string{"a.go": "base a\n"})
	write(t, repo, "a.go", "staged edit\n")
	write(t, repo, "brand-new.go", "staged new file\n")
	if _, err := Git(repo, "add", "a.go", "brand-new.go"); err != nil {
		t.Fatal(err)
	}
	if clean, _ := IndexClean(repo); clean {
		t.Fatal("fixture: index should be dirty before the drain")
	}

	outRoot := t.TempDir()
	t.Setenv(StoreDirEnv, outRoot)
	d := DrainOnStop(repo, "s-worker", "jv-worker")
	if d == nil {
		t.Fatal("expected a drain record for a dirty index")
	}

	clean, err := IndexClean(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("index must be empty after an agent stops")
	}
	// Nothing destroyed: the working tree still holds the content.
	if got := read(t, repo, "a.go"); got != "staged edit\n" {
		t.Fatalf("drain moved working-tree bytes: %q", got)
	}
	if got := read(t, repo, "brand-new.go"); got != "staged new file\n" {
		t.Fatalf("drain lost an untracked file: %q", got)
	}
	// And the staging itself is reversible: the drain saved a patch.
	if d.Patch == "" {
		t.Fatal("drain saved no index patch")
	}
	if _, err := os.Stat(filepath.Join(d.Dir, d.Patch)); err != nil {
		t.Fatalf("drain patch missing: %v", err)
	}
	if d.Agent != "jv-worker" || len(d.Paths) != 2 {
		t.Fatalf("drain record misattributed: %+v", d)
	}

	// ViaDrain touch records must land so attrib list can name the stopping
	// agent for paths that reached the index without a hook observation.
	store := &Store{Root: outRoot}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 ViaDrain records, got %d: %+v", len(records), records)
	}
	for _, r := range records {
		if r.Via != ViaDrain || r.Agent != "jv-worker" || r.Session != "s-worker" {
			t.Fatalf("ViaDrain record mis-shaped: %+v", r)
		}
	}
	dirty, err := DirtyPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	att := Attribute(Resolve(records, nil), dirty)
	slice, ok := att.Slice("jv-worker")
	if !ok || len(slice.Paths(false)) != 2 {
		t.Fatalf("drain must attribute the staged pile to the stopping agent: %+v", att)
	}

	// Idempotent: a second stop finds a clean index and records nothing.
	if d2 := DrainOnStop(repo, "s-worker", "jv-worker"); d2 != nil {
		t.Fatalf("second drain should be a no-op, got %+v", d2)
	}

	// A non-repo workdir is the normal case for non-repo agents: silent nil.
	if d3 := DrainOnStop(t.TempDir(), "s", "a"); d3 != nil {
		t.Fatalf("non-repo drain should be nil, got %+v", d3)
	}
}

func TestT466BackfillTranscripts(t *testing.T) {
	repo := gitRepo(t, map[string]string{"x.go": "base\n"})
	write(t, repo, "x.go", "edited\n")

	// A transcript line the way Claude Code serializes a tool call, plus one
	// path outside the repo that must be dropped.
	transcript := filepath.Join(t.TempDir(), "0fd6c3cd-aaaa-bbbb-cccc-000000000001.jsonl")
	line := `{"timestamp":"2026-08-15T03:04:05Z","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"` +
		filepath.ToSlash(filepath.Join(repo, "x.go")) + `"}}]}}` + "\n" +
		`{"timestamp":"2026-08-15T03:05:05Z","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/etc/hosts"}}]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	store := &Store{Root: t.TempDir()}
	n, err := BackfillTranscripts(store, repo, []string{transcript})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 backfilled record (outside-repo dropped), got %d", n)
	}
	// Idempotent per session: a re-run adds nothing.
	n2, err := BackfillTranscripts(store, repo, []string{transcript})
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("re-run backfilled %d records; must be 0", n2)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Path != "x.go" || records[0].Via != ViaTranscript {
		t.Fatalf("records = %+v", records)
	}
	if records[0].Session != "0fd6c3cd-aaaa-bbbb-cccc-000000000001" {
		t.Fatalf("session should be the file base name, got %q", records[0].Session)
	}
	if !strings.HasPrefix(records[0].At.Format(time.RFC3339), "2026-08-15T03:04:05") {
		t.Fatalf("timestamp not taken from the line: %v", records[0].At)
	}
}
