// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChatTurnsToEvidenceOwnerFriction(t *testing.T) {
	turns := []ChatTurn{
		{Role: "user", Text: "hello", Source: "owner_chat", SourceID: "c1"},
		{Role: "user", Text: "the deploy still not working on iOS", Source: "owner_chat", SourceID: "c2"},
		{Role: "assistant", Text: "I'll fix it", Source: "owner_chat", SourceID: "c3"},
		{Role: "user", Text: "still not working after restart", Source: "owner_chat", SourceID: "c4"},
		{Role: "user", Text: "random update", Source: "owner_chat", SourceID: "c5"},
	}
	ev := ChatTurnsToEvidence(turns)
	if len(ev) != 2 {
		t.Fatalf("want 2 friction evidence rows, got %d %+v", len(ev), ev)
	}
	for _, e := range ev {
		if e.Kind != "chat_gap" {
			t.Errorf("kind=%q want chat_gap", e.Kind)
		}
		if e.Component != "owner_chat" {
			t.Errorf("component=%q", e.Component)
		}
		if e.Fields["phrase"] != "still not working" {
			t.Errorf("phrase=%q", e.Fields["phrase"])
		}
	}
	cands := ExtractCandidates(ev, 2)
	if len(cands) != 1 {
		t.Fatalf("want 1 candidate from repeated chat friction, got %d %+v", len(cands), cands)
	}
	if !strings.Contains(strings.ToLower(cands[0].Name), "owner-chat") {
		t.Errorf("candidate name should cite owner-chat: %q", cands[0].Name)
	}
	if !strings.Contains(cands[0].Context, "T92.2") && !strings.Contains(cands[0].Context, "owner-chat") {
		t.Errorf("context should cite deeper RSI: %q", cands[0].Context)
	}
}

func TestChatTurnsToEvidenceSessionTranscript(t *testing.T) {
	// 🎯T92.2: mnemo/session transcript surface (not only lifecycle JSONL).
	turns := []ChatTurn{
		{Role: "user", Text: "tool call failed to connect", Source: "session", SourceID: "sess-a"},
		{Role: "user", Text: "again: failed to connect to MCP", Source: "session", SourceID: "sess-b"},
		{Role: "error", Text: "panic: nil pointer", Source: "session", SourceID: "sess-c"},
	}
	ev := ChatTurnsToEvidence(turns)
	if len(ev) < 2 {
		t.Fatalf("want session friction evidence, got %+v", ev)
	}
	for _, e := range ev {
		if e.Kind != "transcript_friction" {
			t.Errorf("kind=%q want transcript_friction", e.Kind)
		}
		if e.Component != "session" {
			t.Errorf("component=%q", e.Component)
		}
	}
	// Two "failed to" should cluster; panic is a separate cluster at count 1.
	cands := ExtractCandidates(ev, 2)
	if len(cands) != 1 {
		t.Fatalf("want 1 frequency-qualified session candidate, got %d %+v", len(cands), cands)
	}
	if !strings.Contains(strings.ToLower(cands[0].Name), "session") {
		t.Errorf("name should cite session: %q", cands[0].Name)
	}
}

func TestFrictionSignalIgnoresBenignAgain(t *testing.T) {
	// Bare "again" / smoke "unstuck" must not flood.
	for _, text := range []string{
		"hello again",
		"Say only: unstuck",
		"All gone now. Let's try the whole thing again.",
		"looks good",
	} {
		if ok, _ := frictionSignal(text); ok {
			t.Errorf("false positive friction on %q", text)
		}
	}
	if ok, phrase := frictionSignal("this is broken on master"); !ok || phrase != "this is broken" {
		t.Errorf("want this is broken, got ok=%v phrase=%q", ok, phrase)
	}
}

func TestLoadChatLogTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jevons.jsonl")
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-03T10:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-08-03T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","timestamp":"2026-08-03T10:01:00Z","message":{"role":"user","content":"deploy still broken on device"}}`,
		`{"type":"user","timestamp":"2026-08-03T10:02:00Z","message":{"role":"user","content":"still broken after rebuild"}}`,
		`{"type":"user","timestamp":"2026-08-03T10:03:00Z","message":{"role":"user","content":"[Daemon restart. Your session was rotated]"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	turns, err := LoadChatLogTurns(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	// user lines except harness inject
	if len(turns) != 3 {
		t.Fatalf("want 3 user turns (inject skipped), got %d %+v", len(turns), turns)
	}
	ev := ChatTurnsToEvidence(turns)
	if len(ev) != 2 {
		t.Fatalf("want 2 friction rows from chatlog, got %d %+v", len(ev), ev)
	}
}

func TestLoadSessionTranscriptTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	body := strings.Join([]string{
		`{"type":"system","content":"You are a Grok Build subagent"}`,
		`{"type":"user","content":[{"type":"text","text":"please fix: failed to start worker"}]}`,
		`{"type":"assistant","content":[{"type":"text","text":"working"}]}`,
		`{"type":"user","content":"failed to start worker again after reboot"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	turns, err := LoadSessionTranscriptTurns(path, "sess-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 user turns, got %d %+v", len(turns), turns)
	}
	for _, turn := range turns {
		if turn.Source != "session" || turn.SourceID != "sess-1" {
			t.Errorf("turn source=%q id=%q", turn.Source, turn.SourceID)
		}
	}
}

func TestLoadRecentSessionTurns(t *testing.T) {
	root := t.TempDir()
	// Two session dirs with chat_history.jsonl
	for i, sid := range []string{"aaa", "bbb"} {
		d := filepath.Join(root, "proj", sid)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		// Touch order: sleep not needed if we set mtime via WriteFile order + Chtimes.
		path := filepath.Join(d, "chat_history.jsonl")
		line := `{"type":"user","content":"keeps failing on spawn"}` + "\n"
		if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(path, time.Now().Add(time.Duration(i)*time.Hour), time.Now().Add(time.Duration(i)*time.Hour))
	}
	turns, err := LoadRecentSessionTurns(root, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 session turns, got %d", len(turns))
	}
	ev := ChatTurnsToEvidence(turns)
	if len(ev) != 2 {
		t.Fatalf("want 2 transcript_friction, got %+v", ev)
	}
	cands := ExtractCandidates(ev, 2)
	if len(cands) != 1 {
		t.Fatalf("want 1 candidate, got %+v", cands)
	}
}

func TestRunCycleFromChatLogDeeperPath(t *testing.T) {
	// Hermetic oracle for 🎯T92.2: owner-chat friction → filed target.
	// No lifecycle JSONL evidence at all.
	state := t.TempDir()
	chatPath := filepath.Join(state, "chatlog", "jevons.jsonl")
	if err := os.MkdirAll(filepath.Dir(chatPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for i := 0; i < 3; i++ {
		body += `{"type":"user","message":{"role":"user","content":"the mobile smoke still not working"}}` + "\n"
	}
	if err := os.WriteFile(chatPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mintCwd := t.TempDir()
	if err := writeFile(filepath.Join(mintCwd, "bullseye.yaml"), "schema_version: 1\ntargets: {}\n"); err != nil {
		t.Fatal(err)
	}
	f := &fakeFiler{}
	loop, err := NewLoop(LoopArgs{
		StateDir:     state,
		MintCwd:      mintCwd,
		EventLogPath: filepath.Join(state, "logs", "events.jsonl"), // empty/missing
		ChatLogPath:  chatPath,
		Interval:     -1,
		Filer:        f,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := loop.RunOnce("test-chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Proposed) == 0 {
		t.Fatalf("want proposed from chatlog, got filed=%+v skipped=%+v", res.Filed, res.Skipped)
	}
	if len(res.Filed) != 1 {
		t.Fatalf("want 1 filed from chat friction, got filed=%+v proposed=%+v skipped=%+v",
			res.Filed, res.Proposed, res.Skipped)
	}
	if len(f.calls) != 1 || !strings.Contains(strings.ToLower(f.calls[0].Name), "owner-chat") {
		t.Fatalf("filer call unexpected: %+v", f.calls)
	}
}

func TestRunCycleSessionTranscriptAndMaxMint(t *testing.T) {
	// Deeper extract proposes many distinct clusters; MaxMint still caps (noise control).
	state := t.TempDir()
	sessRoot := filepath.Join(state, "sessions")
	phrases := []string{
		"still not working on a",
		"doesn't work on b",
		"keeps failing on c",
		"this is broken on d",
		"regression on e",
	}
	for i, p := range phrases {
		d := filepath.Join(sessRoot, "p", "s"+string(rune('a'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		// Two of each so frequency threshold passes.
		line := `{"type":"user","content":"` + p + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(d, "chat_history.jsonl"), []byte(line+line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mintCwd := t.TempDir()
	if err := writeFile(filepath.Join(mintCwd, "bullseye.yaml"), "schema_version: 1\ntargets: {}\n"); err != nil {
		t.Fatal(err)
	}
	f := &fakeFiler{}
	loop, err := NewLoop(LoopArgs{
		StateDir:     state,
		MintCwd:      mintCwd,
		EventLogPath: filepath.Join(state, "missing.jsonl"),
		SessionsDir:  sessRoot,
		MaxMint:      2,
		Interval:     -1,
		Filer:        f,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := loop.RunOnce("test-session-noise")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Proposed) < 3 {
		t.Fatalf("deeper extract should propose many, got proposed=%d %+v", len(res.Proposed), res.Proposed)
	}
	if len(res.Filed) != 2 {
		t.Fatalf("MaxMint=2 must cap filing, filed=%d proposed=%d skipped=%+v",
			len(res.Filed), len(res.Proposed), res.Skipped)
	}
	var maxSkip int
	for _, s := range res.Skipped {
		if s.Reason == "max_mint_per_cycle" {
			maxSkip++
		}
	}
	if maxSkip == 0 {
		t.Errorf("expected max_mint_per_cycle skips, skipped=%+v", res.Skipped)
	}
}

func TestRunCycleChatAndEventlogTogether(t *testing.T) {
	state := t.TempDir()
	logPath := filepath.Join(state, "logs", "events.jsonl")
	j, err := openTestJournal(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := j.Append(map[string]any{
			"ts": time.Now().UTC().Format(time.RFC3339Nano), "source": "server",
			"level": "error", "msg": "push failed", "component": "event_push",
			"decision": "push", "fields": map[string]any{"outcome": "error"},
			"corr": "c",
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = j.Close()

	chatPath := filepath.Join(state, "chatlog", "jevons.jsonl")
	if err := os.MkdirAll(filepath.Dir(chatPath), 0o755); err != nil {
		t.Fatal(err)
	}
	chat := `{"type":"user","message":{"role":"user","content":"notification path doesn't work"}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"still doesn't work after restart"}}` + "\n"
	if err := os.WriteFile(chatPath, []byte(chat), 0o644); err != nil {
		t.Fatal(err)
	}

	mintCwd := t.TempDir()
	if err := writeFile(filepath.Join(mintCwd, "bullseye.yaml"), "schema_version: 1\ntargets: {}\n"); err != nil {
		t.Fatal(err)
	}
	f := &fakeFiler{}
	loop, err := NewLoop(LoopArgs{
		StateDir: state, MintCwd: mintCwd, EventLogPath: logPath,
		ChatLogPath: chatPath, MaxMint: 2, Interval: -1, Filer: f,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := loop.RunOnce("both")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Proposed) < 2 {
		t.Fatalf("want eventlog + chat candidates, proposed=%+v", res.Proposed)
	}
	if len(res.Filed) != 2 {
		t.Fatalf("want 2 filed (max), got %+v", res.Filed)
	}
}
