// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/repowatch"
)

// SetRepoWatchAgent attaches the standing PO repo-watch cycle (🎯T1022.2) and
// registers the jevons_repo_watch tool.
func (s *Server) SetRepoWatchAgent(agent *repowatch.Agent) {
	s.mu.Lock()
	s.repowatch = agent
	s.mu.Unlock()
	if agent == nil {
		return
	}
	s.registerRepoWatchTools()
}

// RepoWatchAgent returns the attached repowatch agent (may be nil).
func (s *Server) RepoWatchAgent() *repowatch.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repowatch
}

func (s *Server) registerRepoWatchTools() {
	s.addTool(
		mcp.NewTool("jevons_repo_watch",
			withJobWait(),
			mcp.WithDescription("PO GitHub repo-watch cycle (🎯T1022.2): polls `gh issue list` / `gh pr list` per PO workdir at a bounded cadence and delivers new/updated items to the owning PO as a typed repo-watch brief. action=status (default) reports config and per-repo watermark; action=cycle runs one bounded pass now across every watched repo; action=configure retunes cadence/delivery/opt-outs."),
			mcp.WithString("action", mcp.Description("status (default) | cycle | configure")),
			mcp.WithBoolean("enabled", mcp.Description("configure: enable/disable the standing cycle.")),
			mcp.WithNumber("interval_sec", mcp.Description("configure: cadence in seconds (default 1800 = 30m).")),
			mcp.WithBoolean("deliver", mcp.Description("configure: deliver briefs to the owning PO.")),
			mcp.WithString("disable_repo", mcp.Description("configure: opt a repo out of the watch by name.")),
			mcp.WithString("enable_repo", mcp.Description("configure: remove a repo's opt-out.")),
			mcp.WithString("updated_by", mcp.Description("configure: who is retuning (default jevons).")),
		),
		s.handleRepoWatch,
	)
}

func (s *Server) handleRepoWatch(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.RepoWatchAgent()
	if agent == nil {
		return mcp.NewToolResultError("repowatch agent not configured"), nil
	}
	args := req.GetArguments()
	action := strings.ToLower(strings.TrimSpace(str(args["action"])))
	if action == "" {
		action = "status"
	}
	switch action {
	case "status":
		return mcp.NewToolResultText(s.renderRepoWatchStatus(agent)), nil
	case "cycle":
		return mcp.NewToolResultText(s.dispatchJobWaiting("jevons_repo_watch", s.mcpCallerOf(req), jobWaitArg(req),
			func(_ context.Context) (string, error) {
				return s.runRepoWatchCycle(agent)
			})), nil
	case "configure":
		return mcp.NewToolResultText(s.configureRepoWatch(agent, args)), nil
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown action %q (want status | cycle | configure)", action)), nil
	}
}

func (s *Server) renderRepoWatchStatus(agent *repowatch.Agent) string {
	cfg, err := agent.Config()
	if err != nil {
		return fmt.Sprintf("repowatch config error: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "repowatch (🎯T1022.2): enabled=%v interval=%s deliver=%v disabled_repos=%v\n",
		cfg.Enabled, cfg.IntervalOf(), cfg.Deliver, cfg.DisabledRepos)
	repos := agent.Repos()
	fmt.Fprintf(&b, "watched repos: %d\n", len(repos))
	for _, r := range repos {
		st, err := repowatch.LoadState(agent.StateDir(), r.Name)
		if err != nil {
			fmt.Fprintf(&b, "  %s [%s] owner=%s state error: %v\n", r.Name, r.Dir, r.Owner, err)
			continue
		}
		watermark := "never"
		if !st.Watermark.IsZero() {
			watermark = st.Watermark.Format("2006-01-02T15:04:05Z")
		}
		fmt.Fprintf(&b, "  %s [%s] owner=%s watermark=%s checked=%s last_items=%d%s\n",
			r.Name, r.Dir, r.Owner, watermark, st.CheckedAt, len(st.LastItems), optedOutSuffix(cfg, r.Name))
	}
	return b.String()
}

func optedOutSuffix(cfg repowatch.Config, name string) string {
	if cfg.RepoDisabled(name) {
		return " (opted out)"
	}
	return ""
}

func (s *Server) runRepoWatchCycle(agent *repowatch.Agent) (string, error) {
	res, err := agent.RunOnce("mcp")
	if err != nil {
		s.logLifecycle("repowatch", "cycle", "error", map[string]any{"err": err.Error()})
		return "", fmt.Errorf("repowatch cycle failed: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "repowatch cycle (🎯T1022.2) trigger=%s\n", res.Trigger)
	changed := 0
	for _, rc := range res.Repos {
		if len(rc.New) > 0 {
			changed++
			fmt.Fprintf(&b, "  %s: %d new/updated, delivered=%v watermark=%s\n",
				rc.Repo, len(rc.New), rc.Delivered, rc.Watermark.Format("2006-01-02T15:04:05Z"))
		}
		if rc.Skipped != "" {
			fmt.Fprintf(&b, "  %s: skipped — %s\n", rc.Repo, rc.Skipped)
		}
	}
	if changed == 0 {
		b.WriteString("  nothing new across any watched repo.\n")
	}
	for _, sk := range res.Skipped {
		fmt.Fprintf(&b, "  %s\n", sk)
	}
	s.logLifecycle("repowatch", "cycle", "ok", map[string]any{"changed": changed, "repos": len(res.Repos)})
	return b.String(), nil
}

func (s *Server) configureRepoWatch(agent *repowatch.Agent, args map[string]any) string {
	patch := repowatch.ConfigPatch{UpdatedBy: "jevons"}
	if v := strings.TrimSpace(str(args["updated_by"])); v != "" {
		patch.UpdatedBy = v
	}
	if v, ok := args["enabled"].(bool); ok {
		patch.Enabled = &v
	}
	if v, ok := args["deliver"].(bool); ok {
		patch.Deliver = &v
	}
	if v, ok := args["interval_sec"].(float64); ok {
		n := int(v)
		patch.IntervalSec = &n
	}
	patch.AddDisabled = strings.TrimSpace(str(args["disable_repo"]))
	patch.RemoveEnabled = strings.TrimSpace(str(args["enable_repo"]))

	cfg, err := agent.Config()
	if err != nil {
		return fmt.Sprintf("repowatch configure: load error: %v", err)
	}
	cfg = patch.Apply(cfg)
	if err := repowatch.SaveConfig(agent.StateDir(), cfg); err != nil {
		return fmt.Sprintf("repowatch configure: save error: %v", err)
	}
	s.logLifecycle("repowatch", "configure", "ok", map[string]any{"updated_by": cfg.UpdatedBy})
	return fmt.Sprintf("repowatch config updated: enabled=%v interval=%s deliver=%v disabled_repos=%v\n",
		cfg.Enabled, cfg.IntervalOf(), cfg.Deliver, cfg.DisabledRepos)
}

// poDeliverer fire-and-forget posts a repo-watch brief to a named PO agent
// (🎯T1022.2). Uses sendToAgent (queue/interrupt) — never WaitForResponse,
// same discipline as the research overseerBriefDeliverer.
type poDeliverer struct {
	server *Server
	mu     sync.Mutex
}

// NewPODeliverer builds a repowatch.Deliverer that posts to whichever PO
// agent name it is asked to deliver to.
func (s *Server) NewPODeliverer() repowatch.Deliverer {
	return &poDeliverer{server: s}
}

func (d *poDeliverer) Deliver(target, text string) error {
	if d == nil || d.server == nil {
		return fmt.Errorf("repowatch deliverer: nil server")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("repowatch deliverer: empty target")
	}
	body := butler.FormatEventPush(repowatch.EventSource, text)
	if _, err := d.server.sendToAgent(target, body, false); err != nil {
		if d.server.butler != nil {
			if _, err2 := d.server.butler.PushEvent(target, repowatch.EventSource, text); err2 != nil {
				return fmt.Errorf("repowatch deliver send: %w; push: %v", err, err2)
			}
			return nil
		}
		return err
	}
	return nil
}

// Registry returns the attached fleet registry (may be nil). Used by
// daemon wiring (🎯T1022.2) to discover PO workdirs for repo-watch without
// exposing the private field directly.
func (s *Server) Registry() *claudia.Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registry
}

// DiscoverPORepos builds one RepoSpec per distinct product-owner workdir in
// the live registry (🎯T1022.2): role=="product-owner" (or a name ending in
// "-po", the existing 🎯T511 heuristic) with a non-empty workdir. Repo name
// is the workdir's base directory name. Duplicate workdirs collapse to one
// spec naming the first owner seen.
func DiscoverPORepos(reg *claudia.Registry) []repowatch.RepoSpec {
	if reg == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []repowatch.RepoSpec
	for _, def := range reg.List() {
		if !isPOAgentDef(def) {
			continue
		}
		dir := strings.TrimSpace(def.WorkDir)
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, repowatch.RepoSpec{
			Name:  filepath.Base(clean),
			Dir:   clean,
			Owner: def.Name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func isPOAgentDef(def claudia.AgentDef) bool {
	if strings.EqualFold(strings.TrimSpace(def.Role), "product-owner") {
		return true
	}
	return strings.HasSuffix(strings.TrimSpace(def.Name), "-po")
}
