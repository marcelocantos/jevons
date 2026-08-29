// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package statedb

import (
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestTailStartCountsUserTurns(t *testing.T) {
	s := testStore(t)
	var evs []Event
	idx := 1
	for i := 0; i < 10; i++ {
		evs = append(evs,
			Event{Index: idx, ID: "u:" + itoa(i), Type: "user", Kind: 1, Body: `{"type":"user"}`},
			Event{Index: idx + 1, ID: "a:" + itoa(i), Type: "assistant", Kind: 2, Body: `{"type":"assistant"}`},
			Event{Index: idx + 2, ID: "t:" + itoa(i), Type: "tool_use", Kind: 3, Body: `{"type":"tool_use"}`},
		)
		idx += 3
	}
	if err := s.Upsert("jevons", evs); err != nil {
		t.Fatal(err)
	}
	// 10 user turns, 30 events. Last 3 user turns start at user #8 → idx 22.
	lo, err := s.TailStart("jevons", 3)
	if err != nil || lo != 22 {
		t.Fatalf("TailStart(3)=%d err=%v want 22", lo, err)
	}
	if got, _ := s.TailStart("jevons", 30); got != 1 {
		t.Fatalf("short journal TailStart(30)=%d want 1", got)
	}
	if got, _ := s.TailStart("missing", 30); got != 1 {
		t.Fatalf("empty TailStart=%d", got)
	}
}

func itoa(i int) string {
	if i < 0 {
		return "0"
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func TestOpenMigrateInsertCountRange(t *testing.T) {
	s := testStore(t)
	if err := s.Upsert("jevons", []Event{
		{Index: 1, ID: "e:1", Type: "user", Kind: 1, Body: `{"type":"user"}`},
		{Index: 2, ID: "e:2", Type: "assistant", Kind: 2, Body: `{"type":"assistant"}`},
		{Index: 3, ID: "e:3", Type: "tool_use", Kind: 3, Body: `{"type":"tool_use"}`},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := s.N("jevons")
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	c, err := s.Count("jevons")
	if err != nil || c != 3 {
		t.Fatalf("count=%d err=%v", c, err)
	}
	got, err := s.Range("jevons", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "e:2" || got[1].ID != "e:3" {
		t.Fatalf("range=%+v", got)
	}
	older, err := s.Before("jevons", 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 2 || older[0].Index != 1 || older[1].Index != 2 {
		t.Fatalf("before=%+v", older)
	}
}

func TestUpsertReplacesSameIndex(t *testing.T) {
	s := testStore(t)
	if err := s.Upsert("jevons", []Event{
		{Index: 1, ID: "e:1", Type: "assistant", Body: `{"text":"a"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert("jevons", []Event{
		{Index: 1, ID: "e:1", Type: "assistant", Body: `{"text":"ab"}`},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Range("jevons", 1, 2)
	if err != nil || len(got) != 1 || got[0].Body != `{"text":"ab"}` {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	n, _ := s.N("jevons")
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
}

func TestImportIdempotentWhenRowsExist(t *testing.T) {
	s := testStore(t)
	ok, err := s.ShouldImport("jevons")
	if err != nil || !ok {
		t.Fatalf("empty should import: ok=%v err=%v", ok, err)
	}
	if err := s.ReplaceAll("jevons", []Event{
		{Index: 1, ID: "e:1", Type: "user", Body: `{"type":"user"}`},
	}); err != nil {
		t.Fatal(err)
	}
	ok, err = s.ShouldImport("jevons")
	if err != nil || ok {
		t.Fatalf("rows should skip import: ok=%v err=%v", ok, err)
	}
	if err := s.SetWatermark("jevons", "/tmp/jevons.jsonl", 99, 1); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWatermark("jevons")
	if err != nil || w == nil || w.ImportedN != 1 || w.JSONLSize != 99 {
		t.Fatalf("watermark=%+v err=%v", w, err)
	}
}

func TestDefaultPathAndFileOpen(t *testing.T) {
	dir := t.TempDir()
	path := DefaultPath(dir)
	if filepath.Base(path) != "jevons.db" {
		t.Fatalf("path=%s", path)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAgentUpsertGetListReplace(t *testing.T) {
	s := testStore(t)
	if err := s.UpsertAgent(Agent{
		Name: "jevons-po", Parent: "jevons", Purpose: "work",
		TargetID: "T548", SessionID: "sess", Provider: "grok",
		Model: "grok-4", WorkDir: "/tmp", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgent("jevons-po")
	if err != nil || got == nil || got.TargetID != "T548" || got.Parent != "jevons" {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	if err := s.ReplaceAgents([]Agent{
		{Name: "jevons", Purpose: "overseer", Status: "running"},
		{Name: "jv-t548-statedb", Parent: "jevons-po", Purpose: "work"},
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
	if gone, _ := s.GetAgent("jevons-po"); gone != nil {
		t.Fatal("reaped agent still projected")
	}
}

func TestPruneDuplicateAssistantProseKeepsFirst(t *testing.T) {
	s := testStore(t)
	long := `{"message":{"content":[{"text":"Reproduced: jevons_agent_send routes to my own transcript.\n\nMarcelo — two things need you:"}]}}`
	short := `{"message":{"content":[{"text":"Nothing new to relay."}]}}`
	if err := s.Upsert("jevons", []Event{
		{Index: 1, ID: "u:1", Type: "user", Body: `{"type":"user"}`},
		{Index: 2, ID: "a:1", Type: "assistant", Body: long},
		{Index: 3, ID: "t:1", Type: "tool_use", Body: `{"type":"tool_use"}`},
		{Index: 4, ID: "a:2", Type: "assistant", Body: short},
		{Index: 5, ID: "a:3", Type: "assistant", Body: long},
		{Index: 6, ID: "a:4", Type: "assistant", Body: short},
		{Index: 7, ID: "a:5", Type: "assistant", Body: long},
	}); err != nil {
		t.Fatal(err)
	}
	removed, err := s.PruneDuplicateAssistantProse("jevons")
	if err != nil || removed != 3 {
		t.Fatalf("removed=%d err=%v want 3", removed, err)
	}
	got, err := s.Range("jevons", 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, ev := range got {
		types = append(types, ev.Type)
	}
	want := []string{"user", "assistant", "tool_use", "assistant"}
	if len(types) != 4 || types[0] != want[0] || types[1] != want[1] || types[2] != want[2] || types[3] != want[3] {
		t.Fatalf("remaining types=%v want %v", types, want)
	}
	if AssistantProse(got[1].Type, got[1].Body) == "" || AssistantProse(got[3].Type, got[3].Body) == "" {
		t.Fatal("kept assistant rows lost their prose")
	}
	c, _ := s.Count("jevons")
	n, _ := s.N("jevons")
	if c != 4 || n != 4 {
		t.Fatalf("count=%d n=%d want 4/4", c, n)
	}
}
