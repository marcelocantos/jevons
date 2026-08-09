// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRecord builds one `git log --name-only --pretty=format:%x1e%H%x1f%aI%x1f%s`
// record so the parser is decided without a repository.
func gitRecord(sha, at, subject string, files ...string) string {
	rec := "\x1e" + sha + "\x1f" + at + "\x1f" + subject + "\n"
	for _, f := range files {
		rec += f + "\n"
	}
	return rec
}

func TestParseGitLogRecords(t *testing.T) {
	out := gitRecord("aaaaaaaabbbb", "2026-08-01T10:00:00+10:00", "fix(rsi): stop double delivery",
		"internal/rsi/coach.go", "internal/rsi/coach_test.go") +
		gitRecord("ccccccccdddd", "2026-08-02T10:00:00+10:00", "feat(web): add panel", "web/index.html")

	commits := parseGitLogRecords(out)
	if len(commits) != 2 {
		t.Fatalf("want 2 commits, got %d: %+v", len(commits), commits)
	}
	if commits[0].SHA != "aaaaaaaabbbb" || commits[0].Subject != "fix(rsi): stop double delivery" {
		t.Fatalf("bad header parse: %+v", commits[0])
	}
	if len(commits[0].Files) != 2 {
		t.Fatalf("want 2 files, got %v", commits[0].Files)
	}
	if commits[0].At.IsZero() {
		t.Fatal("commit time not parsed")
	}
}

func TestGitCommitsToEvidenceOnlyRepairHistory(t *testing.T) {
	at := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	commits := []GitCommit{
		{SHA: "1111111111", Subject: "fix(rsi): coach double-delivers", At: at,
			Files: []string{"internal/rsi/coach.go", "internal/rsi/coach_loop.go"}},
		{SHA: "2222222222", Subject: "feat(web): new panel", At: at, Files: []string{"web/index.html"}},
		{SHA: "3333333333", Subject: `Revert "fix(rsi): coach double-delivers"`, At: at,
			Files: []string{"internal/rsi/coach.go"}},
		{SHA: "4444444444", Subject: "chore: bump ledger", At: at, Files: []string{"bullseye.yaml"}},
	}
	ev := GitCommitsToEvidence(commits)

	var rework, revert int
	for _, e := range ev {
		switch e.Kind {
		case "git_rework":
			rework++
			if e.Component != "internal/rsi" {
				t.Fatalf("scope should collapse to two segments: %q", e.Component)
			}
			if e.Fields["source"] != "git" || e.Fields["sha"] == "" {
				t.Fatalf("git evidence needs source+sha pointers: %+v", e.Fields)
			}
		case "git_revert":
			revert++
		default:
			t.Fatalf("unexpected kind %q", e.Kind)
		}
	}
	// One fix commit touching two files in one scope is ONE observation.
	if rework != 1 {
		t.Fatalf("want 1 rework row (per commit+scope), got %d", rework)
	}
	if revert != 1 {
		t.Fatalf("want 1 revert row, got %d", revert)
	}
	// Feature commits and ledger churn are not friction.
	if len(ev) != 2 {
		t.Fatalf("want 2 evidence rows total, got %d: %+v", len(ev), ev)
	}
}

func TestPathScopeAndSubjectShapes(t *testing.T) {
	for path, want := range map[string]string{
		"internal/rsi/coach.go":     "internal/rsi",
		"cmd/jevonsd/main.go":       "cmd/jevonsd",
		"web/scripts/chat.js":       "web/scripts",
		"Makefile":                  "repo-root",
		"docs/design/a/b/c/deep.md": "docs/design",
	} {
		if got := pathScope(path); got != want {
			t.Fatalf("pathScope(%q) = %q, want %q", path, got, want)
		}
	}
	for subject, want := range map[string]bool{
		"fix(sentinel): ignore synthetic rows": true,
		"fix: whatever":                        true,
		"web: kill residual jiggle":            true,
		"feat(rsi): retrospective mine":        false,
		"docs: architecture refresh":           false,
	} {
		if got := isFixSubject(subject); got != want {
			t.Fatalf("isFixSubject(%q) = %v, want %v", subject, got, want)
		}
	}
	if !isRevertSubject(`Revert "fix(x): y"`) || isRevertSubject("reverted expectations doc") {
		t.Fatal("revert subject detection wrong")
	}
}

func TestMineGitCommitsFromRealRepo(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "master")
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		write("internal/flaky/thing.go", fmt.Sprintf("package flaky // v%d\n", i))
		run("add", "-A")
		run("-c", "commit.gpgsign=false", "commit", "-q", "-m",
			fmt.Sprintf("fix(flaky): attempt %d at the same bug", i))
	}

	commits, err := MineGitCommits(repo, time.Now().Add(-time.Hour), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("want 3 commits, got %d", len(commits))
	}
	ev := GitCommitsToEvidence(commits)
	if len(ev) != 3 {
		t.Fatalf("want 3 rework rows, got %d", len(ev))
	}
	cands := ExtractCandidates(ev, retroExtractMinCount)
	if len(cands) != 1 || cands[0].Count != 3 {
		t.Fatalf("want one cluster of 3, got %+v", cands)
	}
	if !strings.Contains(cands[0].Name, "internal/flaky") {
		t.Fatalf("candidate should name the churned scope: %q", cands[0].Name)
	}

	// Non-git directory is silently empty, never an error.
	none, err := MineGitCommits(t.TempDir(), time.Now().Add(-time.Hour), 50)
	if err != nil || len(none) != 0 {
		t.Fatalf("non-repo workdir: commits=%d err=%v", len(none), err)
	}
}

func TestClassifyRetroValueCoarseBar(t *testing.T) {
	cases := []struct {
		name string
		cand Candidate
		want string // "" = accepted
	}{
		{
			name: "history-derived churn clears the bar",
			cand: Candidate{Name: "Repeated fix churn in internal/rsi is diagnosed or eliminated at the root",
				Count: 3, Kinds: []string{"git_rework"}},
		},
		{
			name: "two fix commits are one-off git noise",
			cand: Candidate{Name: "Repeated fix churn in web/scripts is diagnosed or eliminated at the root",
				Count: 2, Kinds: []string{"git_rework"}},
			want: "retro_one_off_git_noise",
		},
		{
			name: "weak phrase needs a big cluster",
			cand: Candidate{Name: "Owner-chat friction (timeout) is reduced or filed as a target",
				Count: 3, Kinds: []string{"chat_gap"}, Phrase: "timeout"},
			want: "retro_weak_phrase_low_count",
		},
		{
			name: "weak phrase at double the coarse count is judged",
			cand: Candidate{Name: "Owner-chat friction (timeout) is reduced or filed as a target",
				Count: 6, Kinds: []string{"chat_gap"}, Phrase: "timeout"},
		},
		{
			name: "bare phrase friction never reaches the overseer",
			cand: Candidate{Name: "friction: timeout", Count: 9, Kinds: []string{"chat_gap"}, Phrase: "timeout"},
			want: "retro_bare_phrase_friction",
		},
		{
			name: "single anecdote",
			cand: Candidate{Name: "Reverted changes in cmd/jevonsd stop recurring (root cause understood)",
				Count: 1, Kinds: []string{"git_revert"}},
			want: "retro_single_anecdote",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRetroValue(tc.cand, DefaultRetroMinCount)
			if tc.want == "" {
				if !got.OK {
					t.Fatalf("want accepted, got %q", got.Reason)
				}
				return
			}
			if got.OK || got.Reason != tc.want {
				t.Fatalf("want reason %q, got OK=%v reason=%q", tc.want, got.OK, got.Reason)
			}
		})
	}
}

// TestRetroCycleJudgesHistoryAndSuppressesNoise is the acceptance hermetic:
// one history-derived judgment delivered with evidence pointers, one low-value
// cluster suppressed, in the same pass.
func TestRetroCycleJudgesHistoryAndSuppressesNoise(t *testing.T) {
	at := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	var ev []Evidence
	// Load-bearing: three independent repair commits in one scope.
	ev = append(ev, GitCommitsToEvidence([]GitCommit{
		{SHA: "aaaa1111", Subject: "fix(chat): jiggle again", At: at, Files: []string{"web/scripts/chat.js"}},
		{SHA: "bbbb2222", Subject: "fix(chat): still jiggling", At: at.Add(time.Hour), Files: []string{"web/scripts/chat.js"}},
		{SHA: "cccc3333", Subject: "fix(chat): kill residual jiggle", At: at.Add(2 * time.Hour), Files: []string{"web/scripts/pin.js"}},
	})...)
	// Noise: two owner-chat turns carrying a weak phrase.
	ev = append(ev, ChatTurnsToEvidence([]ChatTurn{
		{Role: "user", Text: "that timed out again", Source: "owner_chat", SourceID: "jevons.jsonl", TS: at},
		{Role: "user", Text: "it timed out", Source: "owner_chat", SourceID: "jevons.jsonl", TS: at.Add(time.Minute)},
	})...)

	d := &fakeDeliverer{}
	res, err := RunCoachCycle(CoachCycleArgs{
		Evidence:         ev,
		Deliverer:        d,
		MinCount:         retroExtractMinCount,
		Retro:            true,
		RetroCoarseCount: DefaultRetroMinCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Delivered) != 1 {
		t.Fatalf("want exactly 1 history judgment delivered, got %d (judgments=%d skipped=%+v)",
			len(res.Delivered), len(res.Judgments), res.Skipped)
	}
	j := res.Delivered[0]
	if j.Mode != RetroModeLabel {
		t.Fatalf("judgment must be marked retrospective, got mode=%q", j.Mode)
	}
	if !strings.Contains(j.Name, "web/scripts") {
		t.Fatalf("judgment should name the churned scope: %q", j.Name)
	}
	foundSHA := false
	for _, e := range j.Evidence {
		if e.Source == "git" && e.SourceID != "" {
			foundSHA = true
		}
	}
	if !foundSHA {
		t.Fatalf("history judgment needs commit evidence pointers: %+v", j.Evidence)
	}
	msg := d.Messages()[0]
	for _, want := range []string{"Mode: retrospective", "source=git", "Overseer: you alone decide"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("wire text missing %q:\n%s", want, msg)
		}
	}
	suppressed := false
	for _, sk := range res.Skipped {
		if sk.Reason == "retro_weak_phrase_low_count" {
			suppressed = true
		}
	}
	if !suppressed {
		t.Fatalf("low-value chat cluster should be suppressed, skipped=%+v", res.Skipped)
	}
}

// TestCoachRunRetroOnceMinesHistoryWithoutDisturbingDrip wires the whole retro
// path: git + eventlog tail + owner chat, bounded window, durable retro state,
// drip cursor untouched, and no re-delivery on a second pass.
func TestCoachRunRetroOnceMinesHistoryWithoutDisturbingDrip(t *testing.T) {
	state := t.TempDir()
	repo := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "master")
	for i := 0; i < 4; i++ {
		p := filepath.Join(repo, "internal", "churn", "x.go")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(fmt.Sprintf("package churn // %d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "-A")
		run("-c", "commit.gpgsign=false", "commit", "-q", "-m", fmt.Sprintf("fix(churn): attempt %d", i))
	}

	// Journals that predate the coach entirely (drip would never see them).
	logPath := filepath.Join(state, "logs", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	ancient := time.Now().UTC().Add(-100 * 24 * time.Hour).Format(time.RFC3339)
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines, fmt.Sprintf(
			`{"ts":"%s","level":"error","msg":"call failed","component":"mcp","decision":"tool","fields":{"outcome":"error"},"corr":"c%d"}`, old, i))
	}
	// Outside the lookback window: must not be read.
	lines = append(lines, fmt.Sprintf(
		`{"ts":"%s","level":"error","msg":"prehistoric failure","component":"old","decision":"gone","fields":{"outcome":"error"},"corr":"z0"}`, ancient))
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &fakeDeliverer{}
	coach, err := NewCoach(CoachArgs{
		StateDir:     state,
		EventLogPath: logPath,
		Interval:     -1,
		Deliverer:    d,
		RetroWorkdir: repo,
		SeedEOF:      true, // drip starts at EOF: history is the retro pass's job
	})
	if err != nil {
		t.Fatal(err)
	}
	// Rate cap 3 so both history clusters (git churn + eventlog errors) land.
	cap3 := 3
	if _, err := coach.PatchConfig(CoachConfigPatch{RetroRateCap: &cap3, UpdatedBy: "test"}); err != nil {
		t.Fatal(err)
	}

	// Forward drip sees nothing: the cursor was seeded at EOF.
	drip, err := coach.RunOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(drip.Delivered) != 0 {
		t.Fatalf("drip should see no new appends, delivered=%d", len(drip.Delivered))
	}
	curBefore, _ := LoadCoachCursor(state)

	res, err := coach.RunRetroOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Delivered) == 0 {
		t.Fatalf("retro pass must judge history, judgments=%d skipped=%+v", len(res.Judgments), res.Skipped)
	}
	sawGit := false
	for _, j := range res.Delivered {
		if j.Mode != RetroModeLabel {
			t.Fatalf("retro judgment missing mode: %+v", j)
		}
		if strings.Contains(j.Name, "internal/churn") {
			sawGit = true
		}
	}
	if !sawGit {
		t.Fatalf("git churn should surface as a judgment, got %+v", res.Delivered)
	}
	for _, m := range d.Messages() {
		if strings.Contains(m, "prehistoric") {
			t.Fatal("evidence outside the lookback window must not be read")
		}
	}

	// Retro must not advance the drip cursor.
	curAfter, _ := LoadCoachCursor(state)
	if curAfter.ChatLogByte != curBefore.ChatLogByte || curAfter.EventLogByte != curBefore.EventLogByte {
		t.Fatalf("retro disturbed the drip cursor: %+v → %+v", curBefore, curAfter)
	}

	// Durable state records the bounded window (dials in action).
	st, err := coach.RetroState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Runs != 1 || st.LastCommits != 4 || st.LastRunAt == "" || st.LastWindowStart == "" {
		t.Fatalf("retro state not recorded: %+v", st)
	}
	if st.LastEventRows != 3 {
		t.Fatalf("want 3 in-window event rows, got %d", st.LastEventRows)
	}

	// Second pass over the same history re-proposes nothing (judged ledger).
	res2, err := coach.RunRetroOnce("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Delivered) != 0 {
		t.Fatalf("repeat pass must not re-deliver, got %d", len(res2.Delivered))
	}
	priorSkip := false
	for _, sk := range res2.Skipped {
		if sk.Reason == "prior_mint" {
			priorSkip = true
		}
	}
	if !priorSkip {
		t.Fatalf("repeat pass should skip on prior fingerprints: %+v", res2.Skipped)
	}

	// Coach never writes the ledger.
	if _, err := os.Stat(filepath.Join(state, "bullseye.yaml")); !os.IsNotExist(err) {
		t.Fatal("retro coach must not write bullseye")
	}
}

func TestCoachConfigRetroDialsRoundTrip(t *testing.T) {
	state := t.TempDir()
	cfg, err := LoadCoachConfig(state)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RetroEnabled || cfg.RetroIntervalDuration() != DefaultRetroInterval ||
		cfg.RetroLookback() != DefaultRetroLookback {
		t.Fatalf("retro defaults wrong: %+v", cfg)
	}

	off := false
	hours := 48
	interval := 3600
	minCount := 5
	workdir := "/tmp/repo"
	if _, err := PatchCoachConfig(state, CoachConfigPatch{
		RetroEnabled:       &off,
		RetroLookbackHours: &hours,
		RetroIntervalSec:   &interval,
		RetroMinCount:      &minCount,
		RetroWorkdir:       &workdir,
		UpdatedBy:          "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCoachConfig(state)
	if err != nil {
		t.Fatal(err)
	}
	if got.RetroEnabled {
		t.Fatal("explicit retro disable did not survive the round trip")
	}
	if got.RetroLookback() != 48*time.Hour || got.RetroIntervalDuration() != time.Hour ||
		got.EffectiveRetroMinCount() != 5 || got.RetroWorkdir != workdir {
		t.Fatalf("retro dials not persisted: %+v", got)
	}
	// Disabled retro is skipped on the schedule but still runnable by hand.
	w := got.RetroWindowAt(time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC))
	if w.Since != time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("window floor should honour the lookback dial: %v", w.Since)
	}
}

func TestTailEventLogRowsBoundedByWindow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	now := time.Now().UTC()
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf(`{"ts":"%s","level":"error","msg":"recent %d","component":"x"}`,
			now.Add(-time.Duration(i)*time.Hour).Format(time.RFC3339), i))
	}
	lines = append(lines, fmt.Sprintf(`{"ts":"%s","level":"error","msg":"stale","component":"x"}`,
		now.Add(-30*24*time.Hour).Format(time.RFC3339)))
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err := TailEventLogRows(p, 100, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("want 5 in-window rows, got %d", len(rows))
	}
	capped, err := TailEventLogRows(p, 2, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 2 {
		t.Fatalf("max rows dial ignored: got %d", len(capped))
	}
	missing, err := TailEventLogRows(filepath.Join(dir, "nope.jsonl"), 10, now)
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing journal must be empty+nil: %d %v", len(missing), err)
	}
}

// TestJudgmentEvidenceCitesOwnCluster guards the provenance bug the first live
// retro pass exposed: a git_rework judgment about one scope cited commits from
// every other scope, because evidence matched on kind alone.
func TestJudgmentEvidenceCitesOwnCluster(t *testing.T) {
	at := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	// One commit touching two scopes, plus repeat churn in each — so kind and
	// SHA are both shared across clusters and only the cluster key separates them.
	commits := []GitCommit{
		{SHA: "aaaa1111", Subject: "fix(a): one", At: at, Files: []string{"internal/alpha/a.go", "internal/beta/b.go"}},
		{SHA: "bbbb2222", Subject: "fix(a): two", At: at, Files: []string{"internal/alpha/a.go"}},
		{SHA: "cccc3333", Subject: "fix(a): three", At: at, Files: []string{"internal/alpha/a.go"}},
		{SHA: "dddd4444", Subject: "fix(b): two", At: at, Files: []string{"internal/beta/b.go"}},
		{SHA: "eeee5555", Subject: "fix(b): three", At: at, Files: []string{"internal/beta/b.go"}},
	}
	ev := GitCommitsToEvidence(commits)
	cands := ExtractCandidates(ev, retroExtractMinCount)
	if len(cands) != 2 {
		t.Fatalf("want alpha + beta clusters, got %+v", cands)
	}
	for _, c := range cands {
		j := CandidateToJudgment(c, ev)
		want := "internal/alpha"
		if strings.Contains(c.Name, "beta") {
			want = "internal/beta"
		}
		if len(j.Evidence) == 0 {
			t.Fatalf("%q: no evidence pointers", j.Name)
		}
		for _, e := range j.Evidence {
			if !strings.Contains(e.Quote, want) {
				t.Fatalf("%q cites evidence from another cluster: %+v", j.Name, e)
			}
		}
	}
}
