// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/repowatch"
	"github.com/marcelocantos/jevons/internal/roles"
)

// SetRepoWatchAgent attaches the ambient repo-watch cycle (🎯T1022.2) and
// registers jevons_repo_watch + jevons_repo_escalate (🎯T1022.3).
func (s *Server) SetRepoWatchAgent(agent *repowatch.Agent) {
	s.mu.Lock()
	s.repowatch = agent
	s.mu.Unlock()
	if agent == nil {
		return
	}
	s.registerRepoWatchTools()
}

// RepoWatchAgent returns the attached agent (may be nil).
func (s *Server) RepoWatchAgent() *repowatch.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repowatch
}

func (s *Server) registerRepoWatchTools() {
	s.addTool(
		mcp.NewTool("jevons_repo_watch",
			mcp.WithDescription("PO GitHub repo watch (🎯T1022.2): run one poll cycle now, inspect status/config, or retune. Polls `gh issue list` / `gh pr list` per PO workdir at a bounded cadence; new/updated items are delivered as a typed `repo-watch` brief to the owning PO. Hermetic tests use a fake gh runner — live GitHub is never required for CI."),
			mcp.WithString("op", mcp.Description("cycle (default) | status | configure")),
			mcp.WithBoolean("enabled", mcp.Description("configure: enable or disable the standing cycle")),
			mcp.WithNumber("interval_sec", mcp.Description("configure: poll period in seconds (negative disables the ticker)")),
			mcp.WithNumber("max_items_per_repo", mcp.Description("configure: items taken from one repo per cycle (default 50)")),
			mcp.WithString("set_repo", mcp.Description("configure: owner/name to add or update")),
			mcp.WithString("set_workdir", mcp.Description("configure: workdir for set_repo")),
			mcp.WithString("set_po", mcp.Description("configure: PO agent name for set_repo")),
			mcp.WithBoolean("set_enabled", mcp.Description("configure: enabled flag for set_repo (default true)")),
			mcp.WithString("remove_repo", mcp.Description("configure: drop a per-repo override")),
			mcp.WithString("updated_by", mcp.Description("configure: who is retuning (default jevons)")),
		),
		s.handleRepoWatch,
	)
	s.addTool(
		mcp.NewTool("jevons_repo_escalate",
			mcp.WithDescription("Escalate one GitHub issue/PR to the owner via the daemon-side ownerquestions/blurter outbox (🎯T1022.3). Dedup key is repo#number@updated_at — unchanged items do not re-notify; a changed item re-notifies under the existing T1028.3 reminder/coalesce policy. Seats must not call blurter themselves (permission prompt in non-interactive sessions)."),
			mcp.WithString("repo", mcp.Required(), mcp.Description("owner/name")),
			mcp.WithString("kind", mcp.Required(), mcp.Description("issue | pull_request | pr")),
			mcp.WithNumber("number", mcp.Required(), mcp.Description("issue/PR number")),
			mcp.WithString("title", mcp.Required(), mcp.Description("item title")),
			mcp.WithString("url", mcp.Required(), mcp.Description("GitHub URL")),
			mcp.WithString("author", mcp.Description("GitHub login")),
			mcp.WithString("updated_at", mcp.Description("RFC3339 updated_at used in the dedup key (required for stable dedup)")),
			mcp.WithString("ci_state", mcp.Description("success|failure|pending|unknown")),
			mcp.WithNumber("files", mcp.Description("changed file count (PRs)")),
			mcp.WithNumber("additions", mcp.Description("additions (PRs)")),
			mcp.WithNumber("deletions", mcp.Description("deletions (PRs)")),
			mcp.WithString("assessment", mcp.Required(), mcp.Description("PO's one-paragraph assessment")),
			mcp.WithString("recommended_action", mcp.Required(), mcp.Description("what the owner should do")),
			mcp.WithString("answer_route", mcp.Description("reply in owner chat | reply on the GitHub item (default: owner chat)")),
			mcp.WithString("asker", mcp.Description("PO agent name recording this (default: mcp caller)")),
		),
		s.handleRepoEscalate,
	)
}

func (s *Server) handleRepoWatch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.RepoWatchAgent()
	if agent == nil {
		return mcp.NewToolResultError("repowatch agent not configured"), nil
	}
	args := req.GetArguments()
	op := strings.ToLower(strings.TrimSpace(str(args["op"])))
	if op == "" {
		op = "cycle"
	}
	switch op {
	case "cycle":
		res, err := agent.RunOnce(ctx, "mcp")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(formatRepoWatchCycle(res)), nil
	case "status":
		snap, err := agent.Snapshot()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(formatRepoWatchStatus(snap)), nil
	case "configure":
		patch := repowatch.ConfigPatch{UpdatedBy: "jevons"}
		if v := strings.TrimSpace(str(args["updated_by"])); v != "" {
			patch.UpdatedBy = v
		}
		if v, ok := args["enabled"].(bool); ok {
			patch.Enabled = &v
		}
		if v, ok := args["interval_sec"].(float64); ok {
			n := int(v)
			patch.IntervalSec = &n
		}
		if v, ok := args["max_items_per_repo"].(float64); ok {
			n := int(v)
			patch.MaxItemsPerRepo = &n
		}
		if repo := strings.TrimSpace(str(args["set_repo"])); repo != "" {
			rc := repowatch.RepoConfig{
				Repo:    repo,
				Workdir: strings.TrimSpace(str(args["set_workdir"])),
				PO:      strings.TrimSpace(str(args["set_po"])),
			}
			if v, ok := args["set_enabled"].(bool); ok {
				rc.Enabled = &v
			}
			patch.SetRepo = &rc
		}
		patch.RemoveRepo = strings.TrimSpace(str(args["remove_repo"]))
		cfg, err := agent.PatchConfig(patch)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(formatRepoWatchConfig(cfg)), nil
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown op %q (want cycle | status | configure)", op)), nil
	}
}

func formatRepoWatchCycle(res repowatch.CycleResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repo-watch cycle (🎯T1022.2) trigger=%s fresh=%d\n", res.Trigger, res.FreshCount())
	for _, rr := range res.Repos {
		fmt.Fprintf(&b, "  %s po=%s fetched=%d fresh=%d delivered=%v",
			rr.Repo, rr.PO, rr.Fetched, rr.Fresh, rr.Delivered)
		if rr.Skipped != "" {
			fmt.Fprintf(&b, " skipped=%s", rr.Skipped)
		}
		if rr.Error != "" {
			fmt.Fprintf(&b, " err=%s", rr.Error)
		}
		b.WriteByte('\n')
	}
	for _, sk := range res.Skipped {
		fmt.Fprintf(&b, "  skipped: %s\n", sk)
	}
	return b.String()
}

func formatRepoWatchConfig(cfg repowatch.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repo-watch (🎯T1022.2): enabled=%v interval_sec=%d max_items_per_repo=%d\n",
		cfg.Enabled, cfg.IntervalSec, cfg.EffectiveMaxItemsPerRepo())
	fmt.Fprintf(&b, "  updated_at=%s updated_by=%s\n", cfg.UpdatedAt, cfg.UpdatedBy)
	for _, r := range cfg.Repos {
		fmt.Fprintf(&b, "  repo %s enabled=%v po=%s workdir=%s\n",
			r.Repo, r.EnabledOrDefault(), r.PO, r.Workdir)
	}
	return b.String()
}

func formatRepoWatchStatus(snap repowatch.StatusSnapshot) string {
	var b strings.Builder
	b.WriteString(formatRepoWatchConfig(snap.Config))
	fmt.Fprintf(&b, "runs=%d last_run=%s last_trigger=%s last_fresh=%d briefs_sent=%d last_brief=%s last_skip=%s\n",
		snap.State.Runs, snap.State.LastRunAt, snap.State.LastTrigger, snap.State.LastFresh,
		snap.State.BriefsSent, snap.State.LastBriefAt, snap.State.LastSkipReason)
	fmt.Fprintf(&b, "repo_states=%d\n", len(snap.Repos))
	for _, r := range snap.Repos {
		fmt.Fprintf(&b, "  %s po=%s polls=%d delivered=%d last_poll=%s last_error=%s seen=%d\n",
			r.Repo, r.PO, r.Polls, r.Delivered, r.LastPoll, r.LastError, len(r.LastSeen))
	}
	return b.String()
}

func (s *Server) handleRepoEscalate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	repo := strings.TrimSpace(str(args["repo"]))
	kind := strings.ToLower(strings.TrimSpace(str(args["kind"])))
	title := strings.TrimSpace(str(args["title"]))
	url := strings.TrimSpace(str(args["url"]))
	assessment := strings.TrimSpace(str(args["assessment"]))
	rec := strings.TrimSpace(str(args["recommended_action"]))
	if repo == "" || title == "" || url == "" || assessment == "" || rec == "" {
		return mcp.NewToolResultError("repo, title, url, assessment, and recommended_action are required"), nil
	}
	numF, _ := args["number"].(float64)
	number := int(numF)
	if number <= 0 {
		return mcp.NewToolResultError("number must be a positive integer"), nil
	}
	switch kind {
	case "issue":
		// ok
	case "pull_request", "pr", "pull-request":
		kind = "pull_request"
	default:
		return mcp.NewToolResultError("kind must be issue | pull_request"), nil
	}
	updatedAt := strings.TrimSpace(str(args["updated_at"]))
	if updatedAt == "" {
		updatedAt = "unknown"
	}
	// Dedup key: repo#number@updated_at (design doc).
	key := fmt.Sprintf("%s#%d@%s", repo, number, updatedAt)

	author := strings.TrimSpace(str(args["author"]))
	ci := strings.TrimSpace(str(args["ci_state"]))
	route := strings.TrimSpace(str(args["answer_route"]))
	if route == "" {
		route = "reply in owner chat"
	}
	asker := strings.TrimSpace(str(args["asker"]))
	if asker == "" {
		asker = "po"
	}

	var sizeParts []string
	if v, ok := args["files"].(float64); ok && v > 0 {
		sizeParts = append(sizeParts, fmt.Sprintf("files=%.0f", v))
	}
	if v, ok := args["additions"].(float64); ok {
		sizeParts = append(sizeParts, fmt.Sprintf("+%.0f", v))
	}
	if v, ok := args["deletions"].(float64); ok {
		sizeParts = append(sizeParts, fmt.Sprintf("-%.0f", v))
	}
	size := strings.Join(sizeParts, " ")

	var body strings.Builder
	fmt.Fprintf(&body, "Repo-watch escalation (🎯T1022.3)\n")
	fmt.Fprintf(&body, "repo: %s\n", repo)
	fmt.Fprintf(&body, "item: %s #%d %s\n", kind, number, title)
	fmt.Fprintf(&body, "url: %s\n", url)
	if author != "" {
		fmt.Fprintf(&body, "author: %s\n", author)
	}
	if size != "" {
		fmt.Fprintf(&body, "size: %s\n", size)
	}
	if ci != "" {
		fmt.Fprintf(&body, "ci: %s\n", ci)
	}
	fmt.Fprintf(&body, "assessment: %s\n", assessment)
	fmt.Fprintf(&body, "recommended_action: %s\n", rec)
	fmt.Fprintf(&body, "answer_route: %s\n", route)
	fmt.Fprintf(&body, "asker: %s\n", asker)

	dir := s.ownerQuestionsDir
	if dir == "" {
		dir = s.stateDir
	}
	if dir == "" {
		return mcp.NewToolResultError("owner-questions state dir not configured"), nil
	}
	entry, err := ownerquestions.New(dir).Observe(ownerquestions.Question{
		Key:  key,
		Text: body.String(),
		Link: url,
	}, time.Now())
	if err != nil && entry.Status != "spooled" {
		// Observe returns sendErr alongside a durable failed entry; surface both.
		return mcp.NewToolResultError(fmt.Sprintf("escalate failed: status=%s err=%v spool=%s", entry.Status, err, entry.SpoolPath)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "escalated key=%s status=%s\n", key, entry.Status)
	if entry.SpoolPath != "" {
		fmt.Fprintf(&b, "spool_path=%s\n", entry.SpoolPath)
		fmt.Fprintf(&b, "blurter_event_id=%s\n", entry.SpoolPath) // blurter returns the spool ack path/id
	}
	if entry.LastError != "" {
		fmt.Fprintf(&b, "last_error=%s\n", entry.LastError)
	}
	fmt.Fprintf(&b, "delivered=%v (blurter exposes spool ack, not delivery receipt)\n", entry.Delivered)
	s.logLifecycle("repowatch", "escalate", "ok", map[string]any{
		"key": key, "status": entry.Status, "repo": repo, "number": number,
	})
	return mcp.NewToolResultText(b.String()), nil
}

// repoWatchPODeliverer posts typed repo-watch briefs to a named PO.
type repoWatchPODeliverer struct {
	server *Server
	mu     sync.Mutex
}

// NewRepoWatchDeliverer builds a fire-and-forget PO brief deliverer.
func (s *Server) NewRepoWatchDeliverer() repowatch.BriefDeliverer {
	return &repoWatchPODeliverer{server: s}
}

func (d *repoWatchPODeliverer) DeliverRepoWatch(po, text string) error {
	if d == nil || d.server == nil {
		return fmt.Errorf("repowatch deliverer: nil server")
	}
	po = strings.TrimSpace(po)
	if po == "" {
		return fmt.Errorf("repowatch deliverer: empty po")
	}
	body := butler.FormatEventPush(repowatch.EventSource, text)
	res, err := d.server.sendToAgent(po, body, false)
	if err != nil {
		if d.server.butler != nil {
			if _, err2 := d.server.butler.PushEvent(po, repowatch.EventSource, text); err2 != nil {
				return fmt.Errorf("repowatch deliver send: %w; push: %v", err, err2)
			}
			slog.Info("repowatch brief delivered via push fallback", "po", po)
			return nil
		}
		return err
	}
	slog.Info("repowatch brief delivered", "po", po, "status", res.Status)
	return nil
}

// RepoWatchSeatLister adapts the live registry to repowatch.SeatLister.
func (s *Server) RepoWatchSeatLister() repowatch.SeatLister {
	return repowatch.FuncSeatLister(func() []repowatch.Seat {
		if s == nil || s.registry == nil {
			return nil
		}
		defs := s.registry.List()
		out := make([]repowatch.Seat, 0, len(defs))
		for _, d := range defs {
			out = append(out, repowatch.Seat{
				Name:    d.Name,
				WorkDir: d.WorkDir,
				Role:    s.roleOf(d),
				Purpose: string(d.Purpose),
			})
		}
		return out
	})
}

func (s *Server) roleOf(d claudia.AgentDef) string {
	if s == nil {
		return ""
	}
	if d.Role != "" {
		return d.Role
	}
	if s.roleAssignments != nil {
		if r := s.roleAssignments.Get(d.Name); r != "" {
			return r
		}
	}
	return roles.DefaultForPurpose(string(d.Purpose), d.Name)
}
