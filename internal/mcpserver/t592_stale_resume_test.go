// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T592 regression tapes. On 2026-08-31 state_dir/chatlog/jevons.jsonl
// held 5,343 progress frames written after its last type=user record of
// 2026-08-26T08:44 — 🎯T548.2 moved live turns into statedb, but the
// 🎯T328 owner-intent-resume path still read the frozen JSONL, so every
// daemon bounce re-issued a five-day-old Cursor-cycle instruction as
// MANDATORY open owner work.

func t592UserLine(text string, ts time.Time) string {
	return `{"type":"user","timestamp":"` + ts.Format(time.RFC3339) +
		`","message":{"role":"user","content":"` + text + `"}}`
}

// Exercise the restart delivery decision, not just Load+Format composition.
// The ordinary restart notification must still arrive when intent is absent.
func TestRestartBroadcastDoesNotResumeRemovedCanonicalIntent(t *testing.T) {
	for _, state := range []string{"absent", "healthy", "cleared", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			stateDir := t.TempDir()
			now := time.Now().UTC().Truncate(time.Second)
			legacy := "Please implement the legacy instruction for the previous conversation."
			canonical := "Please implement the canonical instruction for this conversation."
			t592WriteChatlog(t, stateDir, []string{t592UserLine(legacy, now)})
			path := statedb.DefaultPath(stateDir)
			if state == "corrupt" {
				if err := os.WriteFile(path, []byte("damaged database"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if state != "absent" {
				db, err := statedb.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Upsert("jevons", []statedb.Event{{Index: 1, Type: "user", Body: t592UserLine(canonical, now)}}); err != nil {
					t.Fatal(err)
				}
				if state == "cleared" {
					if err := db.ReplaceAll("jevons", nil); err != nil {
						t.Fatal(err)
					}
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			s, inbox := t452Fixture(t, "jevons", "recovery-session", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer})
			s.NotifyDaemonRestarted("jevons", "", stateDir)
			msgs := inbox.snapshot()["jevons"]
			if len(msgs) != 1 {
				t.Fatalf("restart delivered %d messages, want one: %v", len(msgs), msgs)
			}
			msg := msgs[0]
			wantIntent := state == "absent" || state == "healthy"
			if strings.Contains(msg, "[event: "+eventOwnerIntentResume+"]") != wantIntent {
				t.Fatalf("state=%s wrong restart event: %s", state, msg)
			}
			if strings.Contains(msg, legacy) != (state == "absent") || strings.Contains(msg, canonical) != (state == "healthy") {
				t.Fatalf("state=%s wrong recovered instruction: %s", state, msg)
			}
			if !wantIntent && !strings.Contains(msg, "[event: "+eventDaemonRestarted+"]") {
				t.Fatalf("ordinary restart notification was lost: %s", msg)
			}
		})
	}
}

func t592ProgressLine(ts time.Time) string {
	return `{"type":"progress","timestamp":"` + ts.Format(time.RFC3339) +
		`","progress_type":"tool_use"}`
}

func t592WriteChatlog(t *testing.T, stateDir string, lines []string) {
	t.Helper()
	dir := filepath.Join(stateDir, "chatlog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "jevons.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The corrected acceptance: when statedb has rows, 🎯T328 reads the turns
// there, not the frozen JSONL — the newest owner instruction is the one
// in the store that is actually recording.
func TestT592ResumeReadsStatedbNotFrozenJSONL(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	old := time.Date(2026, 8, 26, 8, 44, 0, 0, time.UTC)
	now := time.Now().UTC().Truncate(time.Second)

	// The frozen JSONL still holds the five-day-old instruction.
	t592WriteChatlog(t, stateDir, []string{
		t592UserLine("Please run the monthly Cursor cycle now.", old),
	})

	// statedb holds the live turns.
	db, err := statedb.Open(statedb.DefaultPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("jevons", []statedb.Event{
		{Index: 1, TS: now.Add(-10 * time.Minute).Format(time.RFC3339), Type: "user",
			Body: t592UserLine("Please shove the leader research into life-and-work-org-map.md now.", now.Add(-10*time.Minute))},
		{Index: 2, TS: now.Format(time.RFC3339), Type: "progress",
			Body: t592ProgressLine(now)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	got := LoadOpenOwnerIntent(stateDir, "jevons")
	if !got.Recoverable() {
		t.Fatalf("want recoverable from statedb, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "life-and-work-org-map") {
		t.Fatalf("resume recovered %q — the frozen JSONL, not statedb", got.Text)
	}
	if strings.Contains(got.Text, "Cursor cycle") {
		t.Fatalf("resume re-issued the five-day-old instruction: %q", got.Text)
	}
}

// The incident shape with no statedb (pre-🎯T548.2 state dirs, isolates):
// a JSONL whose newest turn lags its newest progress frame by more than
// OpenIntentStaleWindow is a degraded store. No MANDATORY resume — a
// confidently wrong five-day-old instruction is worse than none.
func TestT592FrozenJSONLFallbackIsDegraded(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	old := time.Date(2026, 8, 26, 8, 44, 0, 0, time.UTC)
	t592WriteChatlog(t, stateDir, []string{
		t592UserLine("Please run the monthly Cursor cycle now.", old),
		t592ProgressLine(old.Add(30 * time.Minute)), // within the window: fine
		t592ProgressLine(old.Add(96 * time.Hour)),   // four days of frames, no turn
	})
	got := LoadOpenOwnerIntent(stateDir, "jevons")
	if got.Recoverable() {
		t.Fatalf("degraded store yielded a MANDATORY resume: %q", got.Text)
	}
	if got.Residual != ResidualStaleChatlog {
		t.Fatalf("residual=%q, want %q", got.Residual, ResidualStaleChatlog)
	}
}

// The same degradation inside statedb: if the live store itself stops
// recording turns while frames keep landing, it does not source a resume
// either. The ts column drives staleness even when a frame body carries
// no timestamp of its own.
func TestT592DegradedStatedbRefusesResume(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	old := time.Date(2026, 8, 26, 8, 44, 0, 0, time.UTC)
	db, err := statedb.Open(statedb.DefaultPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("jevons", []statedb.Event{
		{Index: 1, TS: old.Format(time.RFC3339), Type: "user",
			Body: t592UserLine("Please run the monthly Cursor cycle now.", old)},
		{Index: 2, TS: old.Add(96 * time.Hour).Format(time.RFC3339), Type: "progress",
			Body: `{"type":"progress","progress_type":"tool_use"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(stateDir, "jevons")
	if got.Recoverable() {
		t.Fatalf("degraded statedb yielded a MANDATORY resume: %q", got.Text)
	}
	if got.Residual != ResidualStaleChatlog {
		t.Fatalf("residual=%q, want %q", got.Residual, ResidualStaleChatlog)
	}
}

// A healthy statedb keeps the pre-🎯T592 behaviour: recent turn, recent
// frames, recoverable.
func TestT592HealthyStatedbStillResumes(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	db, err := statedb.Open(statedb.DefaultPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("jevons", []statedb.Event{
		{Index: 1, TS: now.Add(-5 * time.Minute).Format(time.RFC3339), Type: "user",
			Body: t592UserLine("Please shove the leader research into life-and-work-org-map.md now.", now.Add(-5*time.Minute))},
		{Index: 2, TS: now.Format(time.RFC3339), Type: "progress",
			Body: t592ProgressLine(now)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(stateDir, "jevons")
	if !got.Recoverable() {
		t.Fatalf("healthy statedb must resume, residual=%q", got.Residual)
	}
}

// Rewind-to-zero leaves a real, authoritative empty journal. Neither an
// empty journal nor an unreadable one permits reviving the old JSONL request.
func TestOpenIntentCanonicalStateNeverFallsBackToRemovedRequest(t *testing.T) {
	for _, state := range []string{"empty", "cleared", "other-agent-only", "corrupt", "directory", "uninitialized-file", "dangling-link", "unreadable-parent"} {
		t.Run(state, func(t *testing.T) {
			stateDir := t.TempDir()
			now := time.Now().UTC().Truncate(time.Second)
			t592WriteChatlog(t, stateDir, []string{
				t592UserLine("Please implement the obsolete request that was removed.", now),
			})
			path := statedb.DefaultPath(stateDir)
			wantResidual := ResidualNoUserTurns
			switch state {
			case "corrupt":
				if err := os.WriteFile(path, []byte("not a SQLite database"), 0o600); err != nil {
					t.Fatal(err)
				}
				wantResidual = ResidualUnreadableChatlog
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				wantResidual = ResidualUnreadableChatlog
			case "uninitialized-file":
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				wantResidual = ResidualUnreadableChatlog
			case "dangling-link":
				if err := os.Symlink(filepath.Join(stateDir, "missing.db"), path); err != nil {
					t.Fatal(err)
				}
				wantResidual = ResidualUnreadableChatlog
			case "unreadable-parent":
				// A filesystem lookup error is not proof of database absence.
				if err := os.Chmod(stateDir, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
				if _, err := os.Stat(path); !os.IsPermission(err) {
					t.Skip("this identity can traverse a directory without permission")
				}
				wantResidual = ResidualUnreadableChatlog
			default:
				db, err := statedb.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if state != "empty" {
					agent := "jevons"
					if state == "other-agent-only" {
						agent = "worker"
					}
					if err := db.Upsert(agent, []statedb.Event{{Index: 1, Type: "user", Body: t592UserLine("Please implement the removed canonical request.", now)}}); err != nil {
						t.Fatal(err)
					}
					if state == "cleared" {
						if err := db.ReplaceAll(agent, nil); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			// The actual restart recovery entry point reopens the database.
			got := LoadOpenOwnerIntent(stateDir, "jevons")
			if got.Recoverable() || got.Text != "" || got.Residual != wantResidual {
				t.Fatalf("%s canonical store recovered obsolete intent: %+v; want %s", state, got, wantResidual)
			}
		})
	}
}
