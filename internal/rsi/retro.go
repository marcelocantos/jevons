// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Retrospective history mining (🎯T353).
//
// Doctrine: fine sensors, coarse conclusions. The drip cursor (🎯T243) observes
// fine-grained new appends; this file adds a *bounded* pass over past evidence —
// git history, the eventlog tail, owner chat, and session transcripts — so the
// coach can judge patterns that predate the cursor. Analysis is batched
// (long interval, bounded window); delivery stays sparse (retro rate cap +
// quality bar + disposition suppressions from 🎯T333).
//
// The retrospective pass never advances the drip cursor and never writes
// bullseye: judgments go to the overseer alone (🎯T243).

const (
	// DefaultRetroInterval is how often the scheduled retrospective pass runs.
	// Deliberately far longer than the drip interval: no full-history re-scan
	// every 15m.
	DefaultRetroInterval = 6 * time.Hour

	// DefaultRetroLookback bounds how far back one retrospective pass reaches.
	DefaultRetroLookback = 7 * 24 * time.Hour

	// DefaultRetroMaxCommits caps the git log window.
	DefaultRetroMaxCommits = 400

	// DefaultRetroMaxEventRows caps eventlog rows scanned per pass.
	DefaultRetroMaxEventRows = 2000

	// DefaultRetroMaxSessions caps session transcript files scanned per pass.
	DefaultRetroMaxSessions = 12

	// DefaultRetroTurnCap caps turns loaded per transcript file.
	DefaultRetroTurnCap = 60

	// DefaultRetroChatTurnCap caps owner-chat turns loaded per pass.
	DefaultRetroChatTurnCap = 200

	// DefaultRetroRateCap is the per-pass delivery cap (sparser than drip).
	DefaultRetroRateCap = 2

	// DefaultRetroMinCount is the coarse-conclusion threshold: how many
	// independent history observations a cluster needs before it is worth an
	// overseer judgment.
	DefaultRetroMinCount = 3

	// retroExtractMinCount is the fine-sensor clustering threshold. Extraction
	// stays sensitive; ClassifyRetroValue applies the coarse bar afterwards.
	retroExtractMinCount = 2

	// retroMaxFilesPerCommit bounds path fan-out from one commit.
	retroMaxFilesPerCommit = 12

	// retroGitTimeout bounds the git log subprocess.
	retroGitTimeout = 30 * time.Second
)

// RetroModeLabel marks judgments produced by a retrospective pass.
const RetroModeLabel = "retro"

// RetroWindow bounds one retrospective pass. Zero fields take package defaults.
type RetroWindow struct {
	// Since is the window floor; evidence older than this is ignored.
	Since time.Time
	// Now is the pass clock (zero = time.Now().UTC()).
	Now time.Time
	// MaxCommits / MaxEventRows / MaxSessions / MaxTurnsPerSession / MaxChatTurns
	// bound each surface so a pass never re-ingests whole histories.
	MaxCommits         int
	MaxEventRows       int
	MaxSessions        int
	MaxTurnsPerSession int
	MaxChatTurns       int
}

func (w RetroWindow) withDefaults() RetroWindow {
	if w.Now.IsZero() {
		w.Now = time.Now().UTC()
	}
	if w.Since.IsZero() {
		w.Since = w.Now.Add(-DefaultRetroLookback)
	}
	if w.MaxCommits <= 0 {
		w.MaxCommits = DefaultRetroMaxCommits
	}
	if w.MaxEventRows <= 0 {
		w.MaxEventRows = DefaultRetroMaxEventRows
	}
	if w.MaxSessions <= 0 {
		w.MaxSessions = DefaultRetroMaxSessions
	}
	if w.MaxTurnsPerSession <= 0 {
		w.MaxTurnsPerSession = DefaultRetroTurnCap
	}
	if w.MaxChatTurns <= 0 {
		w.MaxChatTurns = DefaultRetroChatTurnCap
	}
	return w
}

// RetroGatherArgs names the history surfaces for one bounded pass.
type RetroGatherArgs struct {
	// Workdir is a git repository to mine (empty or non-repo = skip git).
	Workdir string
	// EventLogPath is the event journal (empty = skip).
	EventLogPath string
	// ChatLogPath is the owner main chat journal (empty = skip).
	ChatLogPath string
	// SessionsDir is the provider session store (empty = skip).
	SessionsDir string
	// Window bounds the pass.
	Window RetroWindow
}

// RetroStats reports what one pass actually read (observability + state).
type RetroStats struct {
	Commits        int
	EventRows      int
	ChatTurns      int
	SessionTurns   int
	Evidence       int
	NewestCommit   string
	NewestCommitAt time.Time
	WindowStart    time.Time
}

// GitCommit is one mined commit (fine sensor row).
type GitCommit struct {
	SHA     string
	Subject string
	At      time.Time
	Files   []string
}

// GatherRetroEvidence performs the bounded retrospective mine over git,
// eventlog, owner chat, and session transcripts. Best-effort per surface: an
// unreadable surface is skipped, never fatal — a retrospective pass must not
// take the coach down.
func GatherRetroEvidence(args RetroGatherArgs) ([]Evidence, RetroStats, error) {
	w := args.Window.withDefaults()
	stats := RetroStats{WindowStart: w.Since}
	var ev []Evidence

	if commits, err := MineGitCommits(args.Workdir, w.Since, w.MaxCommits); err == nil && len(commits) > 0 {
		stats.Commits = len(commits)
		stats.NewestCommit = commits[0].SHA
		stats.NewestCommitAt = commits[0].At
		ev = append(ev, GitCommitsToEvidence(commits)...)
	}

	if rows, err := TailEventLogRows(args.EventLogPath, w.MaxEventRows, w.Since); err == nil {
		stats.EventRows = len(rows)
		ev = append(ev, EventsToEvidence(rows)...)
	}

	if turns, err := LoadChatLogTurns(args.ChatLogPath, w.MaxChatTurns); err == nil {
		turns = turnsSince(turns, w.Since)
		stats.ChatTurns = len(turns)
		ev = append(ev, ChatTurnsToEvidence(turns)...)
	}

	if turns, err := LoadRecentSessionTurns(args.SessionsDir, w.MaxSessions, w.MaxTurnsPerSession); err == nil {
		turns = turnsSince(turns, w.Since)
		stats.SessionTurns = len(turns)
		ev = append(ev, ChatTurnsToEvidence(turns)...)
	}

	stats.Evidence = len(ev)
	return ev, stats, nil
}

// turnsSince drops turns older than the window floor. Turns with an unknown
// timestamp are kept: a missing clock is not evidence of age.
func turnsSince(turns []ChatTurn, since time.Time) []ChatTurn {
	if since.IsZero() {
		return turns
	}
	out := turns[:0]
	for _, t := range turns {
		if t.TS.IsZero() || !t.TS.Before(since) {
			out = append(out, t)
		}
	}
	return out
}

// MineGitCommits reads bounded git history from workdir. Returns newest first.
// A missing / non-git / git-less workdir yields no commits and no error.
func MineGitCommits(workdir string, since time.Time, maxCommits int) ([]GitCommit, error) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return nil, nil
	}
	if st, err := os.Stat(filepath.Join(workdir, ".git")); err != nil || st == nil {
		return nil, nil
	}
	if maxCommits <= 0 {
		maxCommits = DefaultRetroMaxCommits
	}
	if since.IsZero() {
		since = time.Now().UTC().Add(-DefaultRetroLookback)
	}

	ctx, cancel := context.WithTimeout(context.Background(), retroGitTimeout)
	defer cancel()
	// \x1e separates commit records; \x1f separates fields within the header.
	cmd := exec.CommandContext(ctx, "git", "-C", workdir, "log",
		"--no-merges",
		"--since="+since.UTC().Format(time.RFC3339),
		"-n", strconv.Itoa(maxCommits),
		"--name-only",
		"--pretty=format:%x1e%H%x1f%aI%x1f%s",
	)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rsi retro: git log: %w", err)
	}
	return parseGitLogRecords(string(out)), nil
}

// parseGitLogRecords parses the \x1e/\x1f record stream from MineGitCommits.
// Pure: hermetic tests decide it without a repository.
func parseGitLogRecords(out string) []GitCommit {
	var commits []GitCommit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		lines := strings.Split(rec, "\n")
		head := strings.Split(lines[0], "\x1f")
		if len(head) < 3 {
			continue
		}
		c := GitCommit{
			SHA:     strings.TrimSpace(head[0]),
			Subject: strings.TrimSpace(head[2]),
		}
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(head[1])); err == nil {
			c.At = t.UTC()
		}
		for _, ln := range lines[1:] {
			ln = strings.TrimSpace(ln)
			if ln == "" || len(c.Files) >= retroMaxFilesPerCommit {
				continue
			}
			c.Files = append(c.Files, ln)
		}
		if c.SHA == "" {
			continue
		}
		commits = append(commits, c)
	}
	return commits
}

// GitCommitsToEvidence maps commits to Evidence rows. Two history signals:
//
//   - git_rework: a fix-shaped commit touching a code scope. One row per
//     (commit, scope) so a single sprawling commit cannot fake a cluster —
//     the cluster count is the number of *independent* fix commits.
//   - git_revert: a revert commit (its own signal at any size).
//
// Non-fix commits are not evidence: ordinary feature history is not friction.
func GitCommitsToEvidence(commits []GitCommit) []Evidence {
	var out []Evidence
	for _, c := range commits {
		sha := shortSHA(c.SHA)
		if isRevertSubject(c.Subject) {
			scope := "repo"
			if scopes := commitScopes(c.Files); len(scopes) > 0 {
				scope = scopes[0]
			}
			out = append(out, Evidence{
				Kind:      "git_revert",
				Component: scope,
				Decision:  "revert",
				Outcome:   "error",
				Message:   "reverted change in " + scope,
				SourceID:  sha,
				TS:        c.At,
				Fields: map[string]string{
					"source":  "git",
					"sha":     sha,
					"subject": c.Subject,
					"phrase":  gitQuote(c.Subject, c.Files),
					"paths":   strings.Join(c.Files, ","),
				},
			})
			continue
		}
		if !isFixSubject(c.Subject) {
			continue
		}
		for _, scope := range commitScopes(c.Files) {
			out = append(out, Evidence{
				Kind:      "git_rework",
				Component: scope,
				Decision:  "fix_churn",
				Outcome:   "error",
				Message:   "repeated fix commits touch " + scope,
				SourceID:  sha,
				TS:        c.At,
				Fields: map[string]string{
					"source":  "git",
					"sha":     sha,
					"subject": c.Subject,
					"phrase":  gitQuote(c.Subject, c.Files),
					"paths":   strings.Join(scopeFiles(c.Files, scope), ","),
				},
			})
		}
	}
	return out
}

// retroIgnoredPaths are churn surfaces that say nothing about product friction:
// the intent ledger moves on every target lifecycle event, and fixtures move
// with the tests that own them.
var retroIgnoredPaths = []string{
	"bullseye.yaml",
	"testdata/",
	"go.sum",
	"package-lock.json",
}

// commitScopes maps a commit's files to deduped directory scopes (≤2 segments).
func commitScopes(files []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, f := range files {
		if ignoredRetroPath(f) {
			continue
		}
		s := pathScope(f)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func scopeFiles(files []string, scope string) []string {
	var out []string
	for _, f := range files {
		if !ignoredRetroPath(f) && pathScope(f) == scope {
			out = append(out, f)
		}
	}
	return out
}

func ignoredRetroPath(f string) bool {
	f = strings.TrimSpace(f)
	if f == "" {
		return true
	}
	for _, ig := range retroIgnoredPaths {
		if f == ig || strings.Contains(f, ig) {
			return true
		}
	}
	return false
}

// pathScope reduces a repo path to at most two directory segments.
func pathScope(p string) string {
	p = strings.TrimSpace(path.Clean(strings.TrimSpace(p)))
	if p == "" || p == "." {
		return ""
	}
	dir := path.Dir(p)
	if dir == "." || dir == "/" {
		return "repo-root"
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, "/")
}

// fixSubjectMarkers are commit-subject shapes that mean "this is repair work".
var fixSubjectMarkers = []string{
	"fix(", "fix:", "fix ", "hotfix", "bugfix", "repair",
	"regression", "still ", "again", "residual", "kill ",
	"broken", "workaround", "patch:",
}

func isFixSubject(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	if s == "" {
		return false
	}
	for _, m := range fixSubjectMarkers {
		if strings.HasPrefix(s, m) || strings.Contains(s, " "+m) {
			return true
		}
	}
	return false
}

func isRevertSubject(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	return strings.HasPrefix(s, "revert ") || strings.HasPrefix(s, `revert"`) || strings.HasPrefix(s, "revert:")
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func gitQuote(subject string, files []string) string {
	q := truncate(subject, 80)
	if len(files) > 0 {
		q += " [" + truncate(strings.Join(files, ","), 60) + "]"
	}
	return q
}

// TailEventLogRows reads the tail of an event journal without touching the drip
// cursor: rows older than since are dropped, and at most maxRows (newest) are
// returned. Missing file → empty, nil error.
func TailEventLogRows(path string, maxRows int, since time.Time) ([]EventRow, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if maxRows <= 0 {
		maxRows = DefaultRetroMaxEventRows
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var rows []EventRow
	sc := newJSONLScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		row, ok := parseEventLine(line)
		if !ok {
			continue
		}
		if !since.IsZero() && row.TS != "" {
			if t, ok := parseEventTS(row.TS); ok && t.Before(since) {
				continue
			}
		}
		rows = append(rows, row)
		// Bound memory: keep a sliding tail rather than the whole journal.
		if len(rows) > maxRows*2 {
			rows = append(rows[:0], rows[len(rows)-maxRows:]...)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}
	return rows, nil
}

func parseEventTS(ts string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// RetroValue is the 🎯T353 coarse-conclusion bar for a retrospective candidate.
type RetroValue struct {
	OK     bool
	Reason string
}

// retroWeakPhrases are friction phrases so common in transcripts that a small
// cluster proves nothing. They need double the coarse count to be judged.
var retroWeakPhrases = []string{
	"error:", "failed to", "timeout", "timed out", "permission denied",
	"going forward", "from now on", "standing rule",
}

// ClassifyRetroValue decides whether a history-derived candidate is worth an
// overseer judgment. Pure: hermetic tests decide it without git or a daemon.
//
// Fine sensors, coarse conclusions — extraction stays sensitive; this bar keeps
// one-off git noise and bare phrase-friction off the overseer's desk.
func ClassifyRetroValue(c Candidate, coarseCount int) RetroValue {
	if coarseCount <= 0 {
		coarseCount = DefaultRetroMinCount
	}
	if IsBarePhraseFrictionName(c.Name) {
		return RetroValue{Reason: "retro_bare_phrase_friction"}
	}
	if c.Count < 2 {
		return RetroValue{Reason: "retro_single_anecdote"}
	}
	kinds := map[string]struct{}{}
	for _, k := range c.Kinds {
		kinds[k] = struct{}{}
	}
	_, rework := kinds["git_rework"]
	_, revert := kinds["git_revert"]
	if rework && !revert && c.Count < coarseCount {
		return RetroValue{Reason: "retro_one_off_git_noise"}
	}
	_, chat := kinds["chat_gap"]
	_, sess := kinds["transcript_friction"]
	if (chat || sess) && isWeakRetroPhrase(c.Phrase) && c.Count < 2*coarseCount {
		return RetroValue{Reason: "retro_weak_phrase_low_count"}
	}
	return RetroValue{OK: true}
}

func isWeakRetroPhrase(phrase string) bool {
	p := strings.ToLower(strings.TrimSpace(phrase))
	if p == "" {
		return false
	}
	for _, w := range retroWeakPhrases {
		if strings.Contains(p, w) {
			return true
		}
	}
	return false
}

// RetroState records what the last retrospective pass covered so status can
// show the dials in action and the schedule can avoid tight re-runs.
// state_dir/rsi/retro_state.json.
type RetroState struct {
	LastRunAt        string `json:"last_run_at,omitempty"`
	LastWindowStart  string `json:"last_window_start,omitempty"`
	LastCommitSHA    string `json:"last_commit_sha,omitempty"`
	LastCommitAt     string `json:"last_commit_at,omitempty"`
	Runs             int    `json:"runs"`
	LastCommits      int    `json:"last_commits"`
	LastEventRows    int    `json:"last_event_rows"`
	LastChatTurns    int    `json:"last_chat_turns"`
	LastSessionTurns int    `json:"last_session_turns"`
	LastJudgments    int    `json:"last_judgments"`
	LastDelivered    int    `json:"last_delivered"`
	LastSuppressed   int    `json:"last_suppressed"`
}

// RetroStatePath is stateDir/rsi/retro_state.json.
func RetroStatePath(stateDir string) string {
	return filepath.Join(stateDir, "rsi", "retro_state.json")
}

// LoadRetroState reads durable retro state; missing → zero value.
func LoadRetroState(stateDir string) (RetroState, error) {
	var st RetroState
	data, err := os.ReadFile(RetroStatePath(stateDir))
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("rsi retro state: read: %w", err)
	}
	if len(bytesTrimSpace(data)) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("rsi retro state: parse: %w", err)
	}
	return st, nil
}

// SaveRetroState atomically persists retro state.
func SaveRetroState(stateDir string, st RetroState) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("rsi retro state: stateDir required")
	}
	dir := filepath.Join(stateDir, "rsi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("rsi retro state: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	p := RetroStatePath(stateDir)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
