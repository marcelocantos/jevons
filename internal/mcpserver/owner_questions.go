// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
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
		mcp.WithDescription("Durable cross-repo owner questions (🎯T404). op=list (default) returns open decisions from ALL repos with each one's silence_outcome — what happens if the owner never answers; op=all includes answered/superseded/moot history. op=record requires explicit typed question identity, asker and answer route, and should carry default (what the asker does on silence) and options (closed-form choices, '|'-separated). op=answer closes that exact version as answered and the DAEMON routes the answer to the asking seat — do not relay it by hand; the result says where it went. op=resolve state=moot|superseded retires a question that no longer needs an answer and tells the asker so. Off-frontier bullseye targets are NOT inferred to be owner questions."),
		mcp.WithString("op", mcp.Description("list | all | record | answer | resolve")),
		mcp.WithString("default", mcp.Description("op=record: what the asker will do if unanswered (🎯T404 default-on-silence). A question without one is listed as STALLS.")),
		mcp.WithString("options", mcp.Description("op=record: closed-form choices, '|'-separated, e.g. 'cockpit|slack'")),
		mcp.WithString("answer", mcp.Description("op=answer: the owner's answer — one of the options, or prose")),
		mcp.WithString("actor", mcp.Description("Your agent name when relaying the owner's answer (default: the overseer name)")),
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
//
// It is the same resolution the typed blocked-report intake uses
// (ownerquestion.CanonicalRepo), so an answer addressed to a question by the
// path its asker wrote finds the row the intake stored (🎯T404). A walk up
// to the nearest `.git` is not that: it sent a non-git directory to "/" and
// answered no question at all.
func canonicalOwnerQuestionRepo(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("repo must be an absolute existing directory")
	}
	return ownerquestion.CanonicalRepo(path)
}

// findOwnerQuestion looks the exact version up under the asker's own repo
// resolution first, then under the worktree-shared resolution the review
// and explicit-blocker intakes record (ownerquestion.SharedRepo).
func findOwnerQuestion(st *ownerquestionview.Store, id ownerquestionview.Identity) (ownerquestionview.Question, error) {
	q, err := st.Find(id)
	if err == nil {
		return q, nil
	}
	shared, serr := ownerquestion.SharedRepo(id.Repo)
	if serr != nil || shared == id.Repo {
		return ownerquestionview.Question{}, err
	}
	id.Repo = shared
	return st.Find(id)
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
			// SilenceOutcome is the 🎯T404 load-bearing field: what happens
			// if the owner never answers. Shown on every row, so a question
			// that stalls its asker is visible as such at a glance.
			SilenceOutcome string `json:"silence_outcome"`
			Notification   string `json:"notification"`
			SpoolPath      string `json:"spool_path,omitempty"`
			LastError      string `json:"last_error,omitempty"`
			Delivered      bool   `json:"delivered"`
		}
		view := make([]displayed, 0, len(rows))
		for _, q := range rows {
			key := q.Identity.Repo + "#" + q.Identity.Target + "#" + q.Identity.ID
			d := displayed{Question: q, SilenceOutcome: q.SilenceOutcome(), Notification: "not-observed"}
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
	case "record", "resolve", "answer":
		repo, err := canonicalOwnerQuestionRepo(str(a["repo"]))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		id := ownerquestionview.Identity{Repo: repo, Target: strings.TrimSpace(str(a["target"])), ID: strings.TrimSpace(str(a["id"])), Version: strings.TrimSpace(str(a["version"]))}
		// The outcome is routed to the asker after the lifecycle write, so a
		// failed write never tells a seat its question was answered.
		var asked ownerquestionview.Question
		var outcomeState ownerquestionview.State
		var outcomeNote string
		recordedText := str(a["text"])
		if op == "record" {
			q := ownerquestionview.Question{Identity: id, Text: recordedText, Asker: str(a["asker"]), AnswerRoute: str(a["answer_route"]), Default: strings.TrimSpace(str(a["default"])), Options: splitQuestionOptions(a["options"])}
			recordedText = ownerquestion.Question{Text: q.Text, Default: q.Default, Options: q.Options}.NoticeBody()
			err = st.Record(q)
		} else {
			asked, err = findOwnerQuestion(st, id)
			if err == nil {
				id = asked.Identity
				if op == "answer" {
					outcomeState = ownerquestionview.Answered
					outcomeNote = strings.TrimSpace(str(a["answer"]))
					if outcomeNote == "" {
						return mcp.NewToolResultError("op=answer requires answer"), nil
					}
					if len(asked.Options) > 0 && !matchesQuestionOption(asked.Options, outcomeNote) {
						// Prose is allowed, but a near-miss on a closed-form
						// option is more likely a typo than a new decision.
						outcomeNote = outcomeNote + " (free-form; options were " + strings.Join(asked.Options, " | ") + ")"
					}
				} else {
					outcomeState = ownerquestionview.State(str(a["state"]))
					outcomeNote = str(a["note"])
				}
				err = st.Resolve(id, outcomeState, outcomeNote)
			}
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		key := repo + "#" + id.Target + "#" + id.ID
		routed := ""
		if op != "record" {
			actor := strings.TrimSpace(str(a["actor"]))
			if actor == "" {
				actor = s.overseerName()
			}
			routed = "\n" + s.routeQuestionOutcome(actor, asked, outcomeState, outcomeNote)
		}
		if op != "record" {
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
				if _, err := ownerquestions.New(s.ownerQuestionsDir).Observe(ownerquestions.Question{Key: key, Text: recordedText}, time.Now()); err != nil {
					return mcp.NewToolResultError("intake saved but notification failed (retry retained): " + err.Error()), nil
				}
			}
		}
		return mcp.NewToolResultText(fmt.Sprintf("Owner question %s/%s/%s@%s %sed in durable index%s", repo, id.Target, id.ID, id.Version, op, routed)), nil
	default:
		return mcp.NewToolResultError("op must be list, all, record, answer or resolve"), nil
	}
}

// splitQuestionOptions reads op=record options as a '|'-separated string or
// a JSON array. Blank entries are dropped; order is the asker's.
func splitQuestionOptions(raw any) []string {
	var parts []string
	switch v := raw.(type) {
	case string:
		parts = strings.Split(v, "|")
	case []any:
		for _, item := range v {
			parts = append(parts, fmt.Sprint(item))
		}
	case []string:
		parts = v
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchesQuestionOption is a case-insensitive exact match against the
// closed-form options; prose answers are still accepted, just labelled.
func matchesQuestionOption(options []string, answer string) bool {
	for _, o := range options {
		if strings.EqualFold(strings.TrimSpace(o), strings.TrimSpace(answer)) {
			return true
		}
	}
	return false
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
	return st.Record(ownerquestionview.Question{Identity: id, Text: q.Text, Asker: q.Asker, AnswerRoute: q.AnswerRoute, Review: review, Default: q.Default, Options: q.Options})
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
