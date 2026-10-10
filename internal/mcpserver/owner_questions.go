// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
	"github.com/mark3labs/mcp-go/mcp"
	"time"
)

// SetOwnerQuestionsDir exposes the durable cross-repo question index. This is
// intentionally a query/intake surface, not T404's future Q&A cockpit UX.
func (s *Server) SetOwnerQuestionsDir(dir string) {
	if s == nil || strings.TrimSpace(dir) == "" {
		return
	}
	s.ownerQuestionsDir = dir
	s.addTool(mcp.NewTool("jevons_owner_questions",
		mcp.WithDescription("Durable cross-repo owner questions. op=list (default) returns open decisions from ALL repos; op=all includes answered/superseded/moot history. op=record requires explicit typed question identity, asker and answer route; op=resolve closes that exact version with a reason. Off-frontier bullseye targets are NOT inferred to be owner questions. This is a query/intake mechanism, not T404's future owner Q&A UI."),
		mcp.WithString("op", mcp.Description("list | all | record | resolve")),
		mcp.WithString("repo", mcp.Description("Absolute repository path (canonicalized to root for record/resolve)")),
		mcp.WithString("target", mcp.Description("Repository-local target id")),
		mcp.WithString("id", mcp.Description("Stable question id within repository+target")),
		mcp.WithString("version", mcp.Description("Content version, changes for a materially different question")),
		mcp.WithString("text", mcp.Description("Question text for op=record")),
		mcp.WithString("asker", mcp.Description("Asker identity for op=record")),
		mcp.WithString("answer_route", mcp.Description("How an answer returns to asker")),
		mcp.WithString("state", mcp.Description("op=resolve: answered | superseded | moot")),
		mcp.WithString("note", mcp.Description("op=resolve: evidence or reason")),
	), s.handleOwnerQuestions)
}
func (s *Server) ownerQuestionStore() (*ownerquestionview.Store, error) {
	if strings.TrimSpace(s.ownerQuestionsDir) == "" {
		return nil, fmt.Errorf("owner questions state_dir not configured")
	}
	return ownerquestionview.New(s.ownerQuestionsDir), nil
}

// canonicalOwnerQuestionRepo refuses nonexistent paths and normalizes git
// worktrees to the repository's checkout root, not a basename: T45 in arr-ai
// and T45 in another repository must never collide.
func canonicalOwnerQuestionRepo(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("repo must be an absolute existing directory")
	}
	abs, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("repo must be a directory")
	}
	// The caller should pass a repo root; accepting nested cwd would otherwise
	// create two identities for the same decision.
	root := abs
	for {
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return root, nil
}
func (s *Server) handleOwnerQuestions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := s.ownerQuestionStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	a := req.GetArguments()
	op := strings.ToLower(strings.TrimSpace(str(a["op"])))
	switch op {
	case "", "list", "all":
		rows, err := st.List(op != "all")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if rows == nil {
			rows = []ownerquestionview.Question{}
		}
		// The notification outbox is a transport state, never an answer state.
		// An absent entry is not proof that the owner was notified.
		notices, digestError, err := ownerquestions.New(s.ownerQuestionsDir).Snapshot()
		if err != nil {
			return mcp.NewToolResultError("notification snapshot: " + err.Error()), nil
		}
		status := map[string]ownerquestions.Entry{}
		for _, n := range notices {
			status[n.Question.Key] = n
		}
		type displayed struct {
			ownerquestionview.Question
			Notification string `json:"notification"`
			SpoolPath    string `json:"spool_path,omitempty"`
			LastError    string `json:"last_error,omitempty"`
			Delivered    bool   `json:"delivered"`
		}
		view := make([]displayed, 0, len(rows))
		for _, q := range rows {
			key := q.Identity.Repo + "#" + q.Identity.Target + "#" + q.Identity.ID
			d := displayed{Question: q, Notification: "not-observed"}
			if n, ok := status[key]; ok && q.State == ownerquestionview.Open {
				d.Notification = n.Status
				d.SpoolPath = n.SpoolPath
				d.LastError = n.LastError
				d.Delivered = n.Delivered
			}
			view = append(view, d)
		}
		data, _ := json.MarshalIndent(view, "", "  ")
		return mcp.NewToolResultText(fmt.Sprintf("Owner questions (%d, %s):\n%s\nDigest error: %s", len(rows), map[bool]string{true: "all states", false: "open"}[op == "all"], data, digestError)), nil
	case "record", "resolve":
		repo, err := canonicalOwnerQuestionRepo(str(a["repo"]))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		id := ownerquestionview.Identity{Repo: repo, Target: strings.TrimSpace(str(a["target"])), ID: strings.TrimSpace(str(a["id"])), Version: strings.TrimSpace(str(a["version"]))}
		if op == "record" {
			err = st.Record(ownerquestionview.Question{Identity: id, Text: str(a["text"]), Asker: str(a["asker"]), AnswerRoute: str(a["answer_route"])})
		} else {
			err = st.Resolve(id, ownerquestionview.State(str(a["state"])), str(a["note"]))
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		key := repo + "#" + id.Target + "#" + id.ID
		if op == "resolve" {
			remaining, err := st.List(true)
			if err != nil {
				return mcp.NewToolResultError("lifecycle saved but open-list failed: " + err.Error()), nil
			}
			anyOpen := false
			for _, q := range remaining {
				if q.Identity.Repo == repo && q.Identity.Target == id.Target && q.Identity.ID == id.ID {
					anyOpen = true
					break
				}
			}
			if !anyOpen {
				if err := ownerquestions.New(s.ownerQuestionsDir).Resolve(key); err != nil {
					return mcp.NewToolResultError("lifecycle saved but notification close failed: " + err.Error()), nil
				}
			}
		} else if s.stateDir != "" {
			open, err := st.OpenVersion(id)
			if err != nil {
				return mcp.NewToolResultError("intake saved but status read failed: " + err.Error()), nil
			}
			if open {
				if _, err := ownerquestions.New(s.ownerQuestionsDir).Observe(ownerquestions.Question{Key: key, Text: str(a["text"])}, time.Now()); err != nil {
					return mcp.NewToolResultError("intake saved but notification failed (retry retained): " + err.Error()), nil
				}
			}
		}
		return mcp.NewToolResultText(fmt.Sprintf("Owner question %s/%s/%s@%s %sed in durable index", repo, id.Target, id.ID, id.Version, op)), nil
	default:
		return mcp.NewToolResultError("op must be list, all, record or resolve"), nil
	}
}

// recordOwnerQuestion stores typed intake without a second notification. The
// notification producer in storeAgentReport owns Observe and dedup.
func (s *Server) recordOwnerQuestion(q ownerquestion.Question) error {
	return s.recordOwnerQuestionWithReview(q, nil)
}

func (s *Server) recordOwnerQuestionWithReview(q ownerquestion.Question, review *ownerquestion.ReviewEvent) error {
	st, err := s.ownerQuestionStore()
	if err != nil {
		return err
	}
	id := ownerquestionview.Identity{Repo: q.Identity.Repo, Target: q.Identity.Target, ID: q.Identity.ID, Version: q.Identity.Version}
	return st.Record(ownerquestionview.Question{Identity: id, Text: q.Text, Asker: q.Asker, AnswerRoute: q.AnswerRoute, Review: review})
}

// resolveGateQuestion closes each recorded version of the gate. A later gate
// record with revised wording can therefore create a fresh open version.
func (s *Server) resolveGateQuestion(repo, target, note string) error {
	st, err := s.ownerQuestionStore()
	if err != nil {
		return err
	}
	rows, err := st.List(true)
	if err != nil {
		return err
	}
	for _, q := range rows {
		if q.Identity.Repo == repo && q.Identity.Target == target && q.Identity.ID == "owner-gate" {
			if err := st.Resolve(q.Identity, ownerquestionview.Answered, note); err != nil {
				return err
			}
		}
	}
	return nil
}
