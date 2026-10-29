// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
	"github.com/mark3labs/mcp-go/mcp"
)

// releaseGrant is an explicit, revocable owner instruction, not a conclusion
// inferred from a chat transcript or an abbreviated testing exception.
type releaseGrant struct {
	Repo              string     `json:"repo"`
	Action            string     `json:"action"`
	By                string     `json:"by"`
	At                time.Time  `json:"at"`
	Evidence          string     `json:"evidence"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	RevokedBy         string     `json:"revoked_by,omitempty"`
	RecordedBy        string     `json:"recorded_by,omitempty"`
	RevokedRecordedBy string     `json:"revoked_recorded_by,omitempty"`
	Pending           bool       `json:"pending,omitempty"`
	// PendingID binds an owner answer to precisely this grant, not a later replacement.
	PendingID string `json:"pending_id,omitempty"`
}

func (s *Server) registerReleaseAuthorizationTool() {
	s.addTool(mcp.NewTool("jevons_release_authorization",
		mcp.WithDescription("Check a durable, revocable owner Ship authorization for a repository. check (default) is read-only and fail-closed; grant/revoke require an explicit owner instruction, actor=owner and evidence quoting/pinpointing it. A minor-release grant covers tag, push and publish of a gated minor release, NOT a patch or major release. Fleet seats must not grant themselves permission. This MCP transport cannot authenticate the caller as owner; overseer must verify the owner instruction before recording."),
		mcp.WithString("op", mcp.Description("check | list | grant | revoke")),
		mcp.WithString("repo", mcp.Description("Absolute existing repository path, including worktrees")),
		mcp.WithString("action", mcp.Description("minor-release | patch-release | major-release (check); only minor-release may be granted without a new scoped contract")),
		mcp.WithString("actor", mcp.Description("grant/revoke: owner (only on actual owner instruction)")),
		mcp.WithString("evidence", mcp.Description("grant/revoke: owner instruction citation, required")),
	), s.handleReleaseAuthorization)
}

func (s *Server) handleReleaseAuthorization(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a := req.GetArguments()
	op := strings.ToLower(strings.TrimSpace(str(a["op"])))
	if op == "" {
		op = "check"
	}
	if op != "check" && op != "list" && op != "grant" && op != "revoke" {
		return mcp.NewToolResultError("op must be check, list, grant or revoke"), nil
	}
	raw := strings.TrimSpace(str(a["repo"]))
	if !filepath.IsAbs(raw) {
		return mcp.NewToolResultError("repo must be an absolute existing repository path"), nil
	}
	repo, err := ownerquestion.SharedRepo(raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	action := strings.ToLower(strings.TrimSpace(str(a["action"])))
	if op == "check" && action == "" {
		return mcp.NewToolResultError("action is required for check"), nil
	}
	if op == "grant" || op == "revoke" {
		if action != "minor-release" {
			return mcp.NewToolResultError("only minor-release grants/revocations are supported"), nil
		}
		if str(a["actor"]) != "owner" || strings.TrimSpace(str(a["evidence"])) == "" {
			return mcp.NewToolResultError("owner instruction required: actor=owner and nonempty evidence"), nil
		}
	}
	if s.ownerQuestionsDir == "" {
		return mcp.NewToolResultError("release authorization state directory not configured"), nil
	}
	writer, worker := s.releaseWriter(ctx, req)
	dir := filepath.Join(s.ownerQuestionsDir, "release-authorizations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	digest := sha256.Sum256([]byte(repo))
	file := filepath.Join(dir, hex.EncodeToString(digest[:])+".json")
	// Lock a separate inode: atomic replacement of the data file must not
	// change the inode on which another process is waiting.
	lock, err := os.OpenFile(file+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	grants := map[string]releaseGrant{}
	b, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &grants); err != nil {
			return mcp.NewToolResultError("corrupt release authorization record: " + err.Error()), nil
		}
	}
	switch op {
	case "check":
		g, ok := grants[action]
		if ok && g.RecordedBy == "" {
			g.RecordedBy = "unknown"
		}
		allowed := ok && g.RevokedAt == nil && !g.Pending && g.Repo == repo && g.By == "owner"
		// Structured response makes a denial unambiguous even on an empty store.
		out := map[string]any{"authorized": allowed, "repo": repo, "action": action}
		if allowed {
			out["record"] = g
		} else {
			out["reason"] = "no active owner authorization for this exact repo and action; file an owner question"
			if ok && g.Pending {
				out["reason"] = "worker grant pending owner confirmation"
				out["record"] = g
			}
		}
		data, _ := json.Marshal(out)
		return mcp.NewToolResultText(string(data)), nil
	case "list":
		for k, g := range grants {
			if g.RecordedBy == "" {
				g.RecordedBy = "unknown"
				grants[k] = g
			}
		}
		data, _ := json.Marshal(grants)
		return mcp.NewToolResultText(string(data)), nil
	case "grant", "revoke":
		now := time.Now().UTC()
		g := grants[action]
		if op == "grant" {
			g = releaseGrant{Repo: repo, Action: action, By: "owner", At: now, Evidence: strings.TrimSpace(str(a["evidence"])), RecordedBy: writer, Pending: worker}
			if worker {
				g.PendingID = fmt.Sprintf("%d", now.UnixNano())
			}
		} else {
			if g.Repo != repo || g.RevokedAt != nil || g.By != "owner" {
				return mcp.NewToolResultError("no active authorization to revoke"), nil
			}
			g.RevokedAt = &now
			g.RevokedBy = "owner: " + strings.TrimSpace(str(a["evidence"]))
			g.RevokedRecordedBy = writer
		}
		grants[action] = g
		data, err := json.MarshalIndent(grants, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		tmp, err := os.CreateTemp(dir, ".release-*")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer os.Remove(tmp.Name())
		if err = tmp.Chmod(0600); err == nil {
			_, err = tmp.Write(data)
		}
		if err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), file)
		}
		if err == nil {
			var d *os.File
			d, err = os.Open(dir)
			if err == nil {
				err = d.Sync()
				d.Close()
			}
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if op == "grant" && worker {
			id := releaseQuestionID(repo, action, g.PendingID)
			st := ownerquestionview.New(s.ownerQuestionsDir)
			q := ownerquestionview.Question{Identity: id, Text: fmt.Sprintf("Confirm minor-release authorization for %s, written by %s? Evidence: %s", repo, writer, g.Evidence), Asker: writer, AnswerRoute: writer, Options: []string{"confirm", "reject"}}
			if err := st.Record(q); err != nil {
				return mcp.NewToolResultError("grant saved pending but owner question intake failed: " + err.Error()), nil
			}
			key := repo + "#" + id.Target + "#" + id.ID
			if _, err := ownerquestions.New(s.ownerQuestionsDir).Observe(ownerquestions.Question{Key: key, Text: q.Text}, now); err != nil {
				return mcp.NewToolResultError("grant saved pending but owner notification failed: " + err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("grant pending owner confirmation: %s (recorded_by=%s)", repo, writer)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("%s %s for %s by owner at %s (recorded_by=%s)", op, action, repo, now.Format(time.RFC3339), writer)), nil
	}
	return mcp.NewToolResultError("unknown operation"), nil
}

// releaseWriter never uses the caller-supplied actor=owner as provenance.
// A seat-pair replay gets its original seat from the durable intent.
//
// "owner-session" is assigned only on a positive overseer/owner-channel
// match (registered overseer seat or that seat's session id). Absence of
// identity is not an owner channel: an unidentified MCP caller is pending.
func (s *Server) releaseWriter(ctx context.Context, req mcp.CallToolRequest) (string, bool) {
	seat := strings.TrimSpace(mcpOriginFrom(ctx).Seat)
	if s.registry != nil {
		if seat != "" {
			for _, d := range s.registry.List() {
				if d.Name == seat {
					return s.releaseIdentity(d.Name, d.Role, d.Purpose)
				}
			}
		}
		if sid := releaseCallerSessionID(req); sid != "" {
			for _, d := range s.registry.List() {
				if d.SessionID == sid {
					return s.releaseIdentity(d.Name, d.Role, d.Purpose)
				}
			}
		}
	}
	if seat != "" {
		return "unknown-seat:" + seat, true
	}
	return "unidentified", true
}

func (s *Server) releaseIdentity(name, role, purpose string) (string, bool) {
	overseer := s.overseerName()
	if role == "overseer" || purpose == "overseer" || name == overseer {
		return "owner-session", false
	}
	return name, role == "worker" || (role == "" && purpose != "overseer" && name != overseer)
}

func releaseCallerSessionID(req mcp.CallToolRequest) string {
	if req.Params.Meta != nil {
		for _, key := range []string{"claudecode/sessionId", "cursor/sessionId", "codex/sessionId", "grok/sessionId"} {
			if v, ok := req.Params.Meta.AdditionalFields[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	if req.Header != nil {
		return strings.TrimSpace(req.Header.Get("X-Claude-Code-Session-Id"))
	}
	return ""
}

func releaseQuestionID(repo, action, version string) ownerquestionview.Identity {
	return ownerquestionview.Identity{Repo: repo, Target: "release-authorization", ID: action, Version: version}
}

// confirmReleaseQuestion is called only after a durable owner-question answer.
// A superseded grant's version cannot authorize a newer grant.
func (s *Server) confirmReleaseQuestion(q ownerquestionview.Question, answer string) error {
	id := q.Identity
	if id.Target != "release-authorization" || id.ID != "minor-release" || !strings.EqualFold(strings.TrimSpace(answer), "confirm") {
		return nil
	}
	digest := sha256.Sum256([]byte(id.Repo))
	file := filepath.Join(s.ownerQuestionsDir, "release-authorizations", hex.EncodeToString(digest[:])+".json")
	lock, err := os.OpenFile(file+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	grants := map[string]releaseGrant{}
	if err := json.Unmarshal(b, &grants); err != nil {
		return err
	}
	g := grants[id.ID]
	if !g.Pending || g.PendingID != id.Version || g.RevokedAt != nil {
		return nil
	}
	g.Pending = false
	grants[id.ID] = g
	b, err = json.MarshalIndent(grants, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".release-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
