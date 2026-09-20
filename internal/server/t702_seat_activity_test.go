// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatactivity"
)

const (
	t702FreshSID  = "019f4f4b-945a-7a23-ba4c-51a0c26e0f10"
	t702StaleSID  = "019f4f4b-945a-7a23-ba4c-51a0c26e0f45"
	t702ClaudeSID = "t702-claude-jsonl"
)

func t702PlantGrok(t *testing.T, sessionsDir, workDir, sid string) string {
	t.Helper()
	dir := filepath.Join(sessionsDir, discovery.EncodeCWDBucket(workDir), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(p, []byte(`{"method":"session/update"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func t702Stamp(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if d := fi.ModTime().Sub(when); d > time.Second || d < -time.Second {
		t.Fatalf("chtimes did not stick on %s: got %s want %s", path, fi.ModTime(), when)
	}
}

func t702GetAgents(t *testing.T, s *Server) []map[string]any {
	t.Helper()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func t702ByName(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("missing %q in /api/agents: %+v", name, rows)
	return nil
}

// TestT702HandlerServesDistinguishableAges is the 🎯T702 hermetic clause:
// fixtures touched 10s and 45min ago are served over GET /api/agents with
// distinguishable ages. Decoding as map[string]any (not agentInfo) is the
// drop-the-field mutation: a missing JSON key is not a zero Go field.
func TestT702HandlerServesDistinguishableAges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	sessions := filepath.Join(t.TempDir(), "sessions")
	homes := t.TempDir()
	now := time.Now()

	freshPath := t702PlantGrok(t, sessions, work, t702FreshSID)
	stalePath := t702PlantGrok(t, discovery.ClaudiaGrokHomeSessionsDir(homes, t702StaleSID), work, t702StaleSID)
	t702Stamp(t, freshPath, now.Add(-10*time.Second))
	t702Stamp(t, stalePath, now.Add(-45*time.Minute))

	claudePath := claudia.SessionJSONLPath(t702ClaudeSID, work)
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t702Stamp(t, claudePath, now.Add(-10*time.Second))

	for _, d := range []claudia.AgentDef{
		{Name: "grok-fresh", WorkDir: work, SessionID: t702FreshSID, Provider: claudia.ProviderGrok},
		{Name: "grok-stale", WorkDir: work, SessionID: t702StaleSID, Provider: claudia.ProviderGrok},
		{Name: "claude-fresh", WorkDir: work, SessionID: t702ClaudeSID, Provider: claudia.ProviderClaude},
		{Name: "cursor-opaque", WorkDir: work, SessionID: t702FreshSID, Provider: claudia.ProviderCursor},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetTranscriptRoots(discovery.Roots{
		GrokSessions:     sessions,
		ClaudiaGrokHomes: homes,
	})

	rows := t702GetAgents(t, s)
	fresh := t702ByName(t, rows, "grok-fresh")
	stale := t702ByName(t, rows, "grok-stale")
	claude := t702ByName(t, rows, "claude-fresh")
	cursor := t702ByName(t, rows, "cursor-opaque")

	t702RequireKnownAge(t, fresh, 10*time.Second, 2*time.Second)
	t702RequireKnownAge(t, stale, 45*time.Minute, 2*time.Second)
	t702RequireKnownAge(t, claude, 10*time.Second, 2*time.Second)
	t702RequireUnknownNullAge(t, cursor)

	freshAge := t702AgeSeconds(t, fresh)
	staleAge := t702AgeSeconds(t, stale)
	if freshAge >= 30*60 {
		t.Fatalf("10s grok seat aged past 30min: %v — supervisor would call it stalled", freshAge)
	}
	if staleAge < 30*60 {
		t.Fatalf("45min grok-homes seat aged under 30min: %v — supervisor would miss the stall", staleAge)
	}
}

// TestT702DecorateRejectsWallClockNow is the substitute-now mutation: if
// decorateSeatActivity (or Lookup) writes time.Now() as last-move / age, the
// 2020 clock below cannot produce 10s and 45min.
func TestT702DecorateRejectsWallClockNow(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	sessions := filepath.Join(t.TempDir(), "sessions")
	now := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	freshPath := t702PlantGrok(t, sessions, work, t702FreshSID)
	stalePath := t702PlantGrok(t, sessions, work, t702StaleSID)
	t702Stamp(t, freshPath, now.Add(-10*time.Second))
	t702Stamp(t, stalePath, now.Add(-45*time.Minute))

	for _, d := range []claudia.AgentDef{
		{Name: "grok-fresh", WorkDir: work, SessionID: t702FreshSID, Provider: claudia.ProviderGrok},
		{Name: "grok-stale", WorkDir: work, SessionID: t702StaleSID, Provider: claudia.ProviderGrok},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", t.TempDir())
	s.SetTranscriptRoots(discovery.Roots{GrokSessions: sessions})
	got := s.decorateSeatActivity(reg, listFleetAgents(reg), now)
	byName := map[string]agentInfo{}
	for _, a := range got {
		byName[a.Name] = a
	}
	fresh := byName["grok-fresh"]
	stale := byName["grok-stale"]
	if fresh.TranscriptActivity != string(seatactivity.VerdictKnown) || stale.TranscriptActivity != string(seatactivity.VerdictKnown) {
		t.Fatalf("fresh=%+v stale=%+v", fresh, stale)
	}
	if fresh.TranscriptAgeSeconds == nil || stale.TranscriptAgeSeconds == nil {
		t.Fatal("known seats must carry transcript_age_seconds, not omit the field")
	}
	if d := absFloat(*fresh.TranscriptAgeSeconds - 10); d > 1 {
		t.Fatalf("fresh age=%v want 10s; wall-clock now was substituted", *fresh.TranscriptAgeSeconds)
	}
	if d := absFloat(*stale.TranscriptAgeSeconds - 45*60); d > 1 {
		t.Fatalf("stale age=%v want 2700s; wall-clock now was substituted", *stale.TranscriptAgeSeconds)
	}
	if fresh.TranscriptLastMove == stale.TranscriptLastMove {
		t.Fatalf("both last_move=%s — substitute-now collapsed the fixtures", fresh.TranscriptLastMove)
	}
}

func t702RequireKnownAge(t *testing.T, row map[string]any, want, slack time.Duration) {
	t.Helper()
	if _, ok := row["transcript_activity"]; !ok {
		t.Fatalf("dropped field transcript_activity in %+v", row)
	}
	if _, ok := row["transcript_age_seconds"]; !ok {
		t.Fatalf("dropped field transcript_age_seconds in %+v", row)
	}
	if _, ok := row["transcript_last_move"]; !ok {
		t.Fatalf("dropped field transcript_last_move in %+v", row)
	}
	if row["transcript_activity"] != "known" {
		t.Fatalf("activity=%v want known in %+v", row["transcript_activity"], row)
	}
	got := t702AgeSeconds(t, row)
	wantSec := want.Seconds()
	if d := absFloat(got - wantSec); d > slack.Seconds() {
		t.Fatalf("age=%v want %v±%v (substitute-now would be ~0) row=%+v", got, want, slack, row)
	}
	move, _ := row["transcript_last_move"].(string)
	parsed, err := time.Parse(time.RFC3339, move)
	if err != nil {
		t.Fatalf("transcript_last_move=%q: %v", move, err)
	}
	if d := time.Since(parsed); d < want-slack || d > want+slack+2*time.Second {
		t.Fatalf("last_move %s is not ~%s ago (wall-clock now substituted?)", move, want)
	}
}

func t702RequireUnknownNullAge(t *testing.T, row map[string]any) {
	t.Helper()
	if _, ok := row["transcript_activity"]; !ok {
		t.Fatalf("dropped field transcript_activity in %+v", row)
	}
	age, ok := row["transcript_age_seconds"]
	if !ok {
		t.Fatalf("dropped field transcript_age_seconds in %+v", row)
	}
	if row["transcript_activity"] != "unknown" {
		t.Fatalf("cursor activity=%v want unknown", row["transcript_activity"])
	}
	if age != nil {
		t.Fatalf("unknown meter must serve transcript_age_seconds=null, not %v (T677)", age)
	}
	if _, ok := row["transcript_last_move"]; ok {
		t.Fatalf("unknown meter must omit last_move, got %+v", row)
	}
}

func t702AgeSeconds(t *testing.T, row map[string]any) float64 {
	t.Helper()
	switch v := row["transcript_age_seconds"].(type) {
	case float64:
		return v
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			t.Fatal(err)
		}
		return f
	default:
		t.Fatalf("transcript_age_seconds=%T %v want number", v, v)
		return 0
	}
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
