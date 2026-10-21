// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ExecRunner shells out to the real `gh` binary. Production default.
type ExecRunner struct{}

// Run executes `gh <args...>` with dir as the working directory.
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// ghIssueJSON mirrors the `gh issue list --json` fields this package uses.
type ghIssueJSON struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"`
	UpdatedAt string `json:"updatedAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
}

// ghPRJSON mirrors the `gh pr list --json` fields this package uses.
type ghPRJSON struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"`
	UpdatedAt string `json:"updatedAt"`
	IsDraft   bool   `json:"isDraft"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Files []struct {
		Path string `json:"path"`
	} `json:"files"`
	StatusCheckRollup []struct {
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

// issueFields / prFields are the `--json` field lists requested of `gh`.
const issueFields = "number,title,url,state,updatedAt,author"
const prFields = "number,title,url,state,updatedAt,author,isDraft,additions,deletions,files,statusCheckRollup"

// ListIssues runs `gh issue list --json ... --state all` in dir and parses
// the result into Items. Open issues only is the common case, but `--state
// all` lets a just-closed issue still surface as "updated" once, which keeps
// the PO brief honest about a closure it did not itself cause.
func ListIssues(ctx context.Context, runner GhRunner, dir string) ([]Item, error) {
	out, err := runner.Run(ctx, dir, "issue", "list", "--state", "all", "--limit", "200", "--json", issueFields)
	if err != nil {
		return nil, err
	}
	var raw []ghIssueJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse gh issue list: %w", err)
	}
	items := make([]Item, 0, len(raw))
	for _, r := range raw {
		items = append(items, Item{
			Kind:      "issue",
			Number:    r.Number,
			Title:     r.Title,
			URL:       r.URL,
			Author:    r.Author.Login,
			State:     strings.ToLower(r.State),
			UpdatedAt: r.UpdatedAt,
		})
	}
	return items, nil
}

// ListPRs runs `gh pr list --json ...` in dir and parses the result into
// Items, including CI rollup and size.
func ListPRs(ctx context.Context, runner GhRunner, dir string) ([]Item, error) {
	out, err := runner.Run(ctx, dir, "pr", "list", "--state", "all", "--limit", "200", "--json", prFields)
	if err != nil {
		return nil, err
	}
	var raw []ghPRJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	items := make([]Item, 0, len(raw))
	for _, r := range raw {
		items = append(items, Item{
			Kind:      "pr",
			Number:    r.Number,
			Title:     r.Title,
			URL:       r.URL,
			Author:    r.Author.Login,
			State:     strings.ToLower(r.State),
			UpdatedAt: r.UpdatedAt,
			Draft:     r.IsDraft,
			Additions: r.Additions,
			Deletions: r.Deletions,
			Files:     len(r.Files),
			CIState:   prCIState(r.StatusCheckRollup),
		})
	}
	return items, nil
}

func prCIState(rollup []struct {
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}) string {
	if len(rollup) == 0 {
		return "none"
	}
	allSuccess := true
	anyFail := false
	for _, c := range rollup {
		state := strings.ToUpper(c.Conclusion)
		if state == "" {
			state = strings.ToUpper(c.State)
		}
		switch state {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT":
			anyFail = true
			allSuccess = false
		default:
			allSuccess = false
		}
	}
	switch {
	case anyFail:
		return "failing"
	case allSuccess:
		return "green"
	default:
		return "pending"
	}
}

// RenderBrief formats a repo-watch brief for delivery to a PO (🎯T1022.2).
// The PO applies the 🎯T1022.3 triage doctrine to each item; this is
// awareness, not judgment.
func RenderBrief(repo string, items []Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "repo-watch: %s — %d new/updated item(s)\n", repo, len(items))
	for _, it := range items {
		switch it.Kind {
		case "pr":
			draft := ""
			if it.Draft {
				draft = " draft"
			}
			fmt.Fprintf(&b, "  PR #%d%s [%s] %s (+%d/-%d, %d files, ci=%s) %s — %s\n",
				it.Number, draft, it.State, it.Title, it.Additions, it.Deletions, it.Files, it.CIState, it.Author, it.URL)
		default:
			fmt.Fprintf(&b, "  Issue #%d [%s] %s — %s — %s\n", it.Number, it.State, it.Title, it.Author, it.URL)
		}
	}
	return b.String()
}

