// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ItemKind is issue or pull_request.
type ItemKind string

const (
	KindIssue ItemKind = "issue"
	KindPR    ItemKind = "pull_request"
)

// Item is one GitHub issue or pull request as seen by a poll.
type Item struct {
	Kind      ItemKind  `json:"kind"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Author    string    `json:"author,omitempty"`
	State     string    `json:"state,omitempty"`
	Draft     bool      `json:"draft,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	// FilesChanged / Additions / Deletions are PR-only when available.
	FilesChanged int    `json:"files_changed,omitempty"`
	Additions    int    `json:"additions,omitempty"`
	Deletions    int    `json:"deletions,omitempty"`
	CIState      string `json:"ci_state,omitempty"` // success|failure|pending|unknown|""
	// Labels is issue/PR label names.
	Labels []string `json:"labels,omitempty"`
	// IsBot is true when the author is a known bot (dependabot, renovate).
	IsBot bool `json:"is_bot,omitempty"`
}

// Key is the stable item identity within a repo (kind+number).
func (it Item) Key() string {
	return fmt.Sprintf("%s#%d", it.Kind, it.Number)
}

// Runner executes gh against a workdir. Injectable so hermetic tests never
// hit the network (🎯T1022.2 acceptance).
type Runner interface {
	// Run returns stdout of `gh <args...>` with Dir=workdir.
	Run(ctx context.Context, workdir string, args ...string) ([]byte, error)
}

// ExecRunner is the shipped runner: real `gh` on PATH.
type ExecRunner struct {
	// Bin defaults to "gh".
	Bin string
}

// Run executes gh.
func (r ExecRunner) Run(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "gh"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// FakeRunner is a hermetic Runner keyed by joined args.
type FakeRunner struct {
	// Responses maps strings.Join(args, " ") → body (or error via Errors).
	Responses map[string][]byte
	Errors    map[string]error
	// Calls records every invocation for assertions.
	Calls [][]string
}

// Run returns a canned response.
func (f *FakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("nil FakeRunner")
	}
	key := strings.Join(args, " ")
	f.Calls = append(f.Calls, append([]string(nil), args...))
	if err, ok := f.Errors[key]; ok {
		return nil, err
	}
	if body, ok := f.Responses[key]; ok {
		return body, nil
	}
	// Prefix match: allow tests to key only the subcommand.
	for k, body := range f.Responses {
		if strings.HasPrefix(key, k) {
			return body, nil
		}
	}
	return nil, fmt.Errorf("fake gh: no response for %q", key)
}

// gh issue/pr list JSON shapes (subset of gh --json fields).
type ghListRow struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"`
	IsDraft   bool   `json:"isDraft"`
	UpdatedAt string `json:"updatedAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	// PR-only:
	Additions      int `json:"additions"`
	Deletions      int `json:"deletions"`
	ChangedFiles   int `json:"changedFiles"`
	StatusCheckRollup []struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

// ListIssues runs `gh issue list --json ...` and returns open issues.
func ListIssues(ctx context.Context, r Runner, workdir string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = DefaultMaxItemsPerRepo
	}
	args := []string{
		"issue", "list",
		"--state", "open",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "number,title,url,state,updatedAt,author,labels",
	}
	body, err := r.Run(ctx, workdir, args...)
	if err != nil {
		return nil, err
	}
	return parseList(body, KindIssue)
}

// ListPRs runs `gh pr list --json ...` and returns open PRs.
func ListPRs(ctx context.Context, r Runner, workdir string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = DefaultMaxItemsPerRepo
	}
	args := []string{
		"pr", "list",
		"--state", "open",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "number,title,url,state,isDraft,updatedAt,author,labels,additions,deletions,changedFiles,statusCheckRollup",
	}
	body, err := r.Run(ctx, workdir, args...)
	if err != nil {
		return nil, err
	}
	return parseList(body, KindPR)
}

func parseList(body []byte, kind ItemKind) ([]Item, error) {
	var rows []ghListRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("parse gh %s list: %w", kind, err)
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		it := Item{
			Kind:         kind,
			Number:       row.Number,
			Title:        strings.TrimSpace(row.Title),
			URL:          strings.TrimSpace(row.URL),
			Author:       strings.TrimSpace(row.Author.Login),
			State:        strings.ToLower(strings.TrimSpace(row.State)),
			Draft:        row.IsDraft,
			UpdatedAt:    parseGHTime(row.UpdatedAt),
			FilesChanged: row.ChangedFiles,
			Additions:    row.Additions,
			Deletions:    row.Deletions,
			IsBot:        isBotLogin(row.Author.Login),
		}
		for _, l := range row.Labels {
			if n := strings.TrimSpace(l.Name); n != "" {
				it.Labels = append(it.Labels, n)
			}
		}
		if kind == KindPR {
			it.CIState = rollupCI(row.StatusCheckRollup)
		}
		out = append(out, it)
	}
	return out, nil
}

func rollupCI(checks []struct {
	State string `json:"state"`
}) string {
	if len(checks) == 0 {
		return "unknown"
	}
	// Worst-wins: failure > pending > success.
	worst := "success"
	for _, c := range checks {
		s := strings.ToUpper(strings.TrimSpace(c.State))
		switch s {
		case "FAILURE", "ERROR", "CANCELLED":
			return "failure"
		case "PENDING", "QUEUED", "IN_PROGRESS", "EXPECTED":
			worst = "pending"
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			// keep
		default:
			if worst == "success" {
				worst = "unknown"
			}
		}
	}
	return worst
}

func isBotLogin(login string) bool {
	l := strings.ToLower(strings.TrimSpace(login))
	switch {
	case l == "":
		return false
	case strings.HasSuffix(l, "[bot]"):
		return true
	case strings.HasSuffix(l, "-bot"):
		return true
	case l == "dependabot" || l == "renovate" || l == "github-actions":
		return true
	default:
		return false
	}
}

func parseGHTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// ResolveRepo asks `gh repo view --json nameWithOwner` for the workdir's origin.
func ResolveRepo(ctx context.Context, r Runner, workdir string) (string, error) {
	body, err := r.Run(ctx, workdir, "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		// Fall back without -q for older gh: parse JSON.
		body, err = r.Run(ctx, workdir, "repo", "view", "--json", "nameWithOwner")
		if err != nil {
			return "", err
		}
		var v struct {
			NameWithOwner string `json:"nameWithOwner"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return "", fmt.Errorf("parse repo view: %w", err)
		}
		return strings.TrimSpace(v.NameWithOwner), nil
	}
	return strings.TrimSpace(string(body)), nil
}
