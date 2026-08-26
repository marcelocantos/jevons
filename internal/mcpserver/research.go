// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/research"
)

// SetResearchAgent attaches the ambient research staff cycle (🎯T356) and
// registers the jevons_research_* tools. Notes are durable and versioned; the
// cycle briefs the overseer but never files bullseye itself.
func (s *Server) SetResearchAgent(agent *research.Agent) {
	s.mu.Lock()
	s.research = agent
	s.mu.Unlock()
	if agent == nil {
		return
	}
	s.registerResearchTools()
}

// ResearchAgent returns the attached research agent (may be nil).
func (s *Server) ResearchAgent() *research.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.research
}

func (s *Server) registerResearchTools() {
	s.addTool(
		mcp.NewTool("jevons_research_cycle",
			mcp.WithDescription("Run one ambient research cycle now (🎯T356). context: explore recent work across repos, the frontier, the eventlog and sessions. feed: poll subscribed news feeds and fold new items in. Findings land in durable versioned notes — prior conclusions are superseded explicitly, never overwritten. A cycle that finds nothing new writes no revision and sends no brief."),
			mcp.WithString("mode", mcp.Description("context (default) | feed | both")),
		),
		s.handleResearchCycle,
	)
	s.addTool(
		mcp.NewTool("jevons_research_list",
			mcp.WithDescription("List durable research notes (🎯T356): topic, revision count, last update, and the markdown path for each. This is the listable surface for root/PO — read one with jevons_research_read."),
		),
		s.handleResearchList,
	)
	s.addTool(
		mcp.NewTool("jevons_research_read",
			mcp.WithDescription("Read one research note (🎯T356) as markdown: current findings with provenance, superseded conclusions, and revision history."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Note id or topic from jevons_research_list (e.g. repo-jevons or repo/jevons)")),
		),
		s.handleResearchRead,
	)
	s.addTool(
		mcp.NewTool("jevons_research_configure",
			mcp.WithDescription("Retune the ambient research cycle (🎯T356): cadence, lookback, delivery budget, and feed subscriptions. Feeds are explicit — the agent fetches exactly the subscribed URLs and never crawls out of them; a host outside allowed_hosts is never fetched."),
			mcp.WithBoolean("enabled", mcp.Description("Enable or disable the standing cycle.")),
			mcp.WithNumber("interval_sec", mcp.Description("Context-refresh period in seconds (negative disables the ticker).")),
			mcp.WithNumber("lookback_hours", mcp.Description("How far back one context pass reaches (default 72).")),
			mcp.WithNumber("max_commits", mcp.Description("Commits mined per repo per pass (default 300).")),
			mcp.WithNumber("max_repos", mcp.Description("Sibling repositories scanned per pass (default 6).")),
			mcp.WithString("workdir", mcp.Description("Primary repository explored each pass (empty clears to the daemon workdir).")),
			mcp.WithString("related_roots", mcp.Description("Comma-separated org roots scanned for sibling repos. Empty string clears.")),
			mcp.WithBoolean("deliver", mcp.Description("Deliver research briefs to the overseer when a cycle finds something new.")),
			mcp.WithString("overseer", mcp.Description("Brief delivery target agent (default jevons).")),
			mcp.WithNumber("max_briefs_per_hour", mcp.Description("Delivery budget (default 2).")),
			mcp.WithBoolean("feed_enabled", mcp.Description("Enable the async feed trigger path.")),
			mcp.WithNumber("feed_interval_sec", mcp.Description("Feed poll period in seconds (default 1800).")),
			mcp.WithNumber("max_feed_items", mcp.Description("Items taken from one feed per cycle (default 10).")),
			mcp.WithNumber("max_feed_cycles_per_hour", mcp.Description("Feed-triggered cycle budget (default 4).")),
			mcp.WithString("add_feed_name", mcp.Description("Subscribe (or update) a feed by name; requires add_feed_url.")),
			mcp.WithString("add_feed_url", mcp.Description("Feed URL (http/https). Subscribing also allows its host.")),
			mcp.WithString("add_feed_topic", mcp.Description("Optional note topic for the feed (default feed/<name>).")),
			mcp.WithBoolean("add_feed_enabled", mcp.Description("Whether the added feed is polled (default true).")),
			mcp.WithString("remove_feed", mcp.Description("Unsubscribe a feed by name.")),
			mcp.WithString("allowed_hosts", mcp.Description("Comma-separated fetch allowlist. Empty string clears (which allows nothing).")),
			mcp.WithString("updated_by", mcp.Description("Who is retuning (default jevons).")),
		),
		s.handleResearchConfigure,
	)
	s.addTool(
		mcp.NewTool("jevons_research_status",
			mcp.WithDescription("Show the ambient research cycle's config, run record, note count, and per-feed poll cursors (🎯T356)."),
		),
		s.handleResearchStatus,
	)
}

func (s *Server) handleResearchCycle(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.ResearchAgent()
	if agent == nil {
		return mcp.NewToolResultError("research agent not configured"), nil
	}
	mode := "context"
	if v := strings.TrimSpace(str(req.GetArguments()["mode"])); v != "" {
		mode = strings.ToLower(v)
	}
	switch mode {
	case "context", "feed", "both":
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown mode %q (want context | feed | both)", mode)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Research cycle (🎯T356) mode=%s\n", mode)
	changed := 0
	if mode == "context" || mode == "both" {
		res, err := agent.RunOnce("mcp")
		if err != nil {
			s.logLifecycle("research", "cycle", "error", map[string]any{"err": err.Error(), "mode": mode})
			return mcp.NewToolResultError(fmt.Sprintf("research cycle failed: %v", err)), nil
		}
		changed += writeResearchCycle(&b, res)
	}
	if mode == "feed" || mode == "both" {
		poll, err := agent.PollFeeds(ctx, "mcp")
		if err != nil {
			s.logLifecycle("research", "feed_cycle", "error", map[string]any{"err": err.Error()})
			return mcp.NewToolResultError(fmt.Sprintf("research feed poll failed: %v", err)), nil
		}
		fmt.Fprintf(&b, "  feeds polled: %d\n", poll.Polled)
		for _, res := range poll.Cycles {
			changed += writeResearchCycle(&b, res)
		}
		for _, sk := range poll.Skipped {
			fmt.Fprintf(&b, "  skipped: %s\n", sk)
		}
	}
	if changed == 0 {
		b.WriteString("  nothing new — notes unchanged, no brief sent.\n")
	}
	s.logLifecycle("research", "cycle", "ok", map[string]any{"mode": mode, "changed": changed})
	return mcp.NewToolResultText(b.String()), nil
}

func writeResearchCycle(b *strings.Builder, res research.CycleResult) int {
	changed := 0
	for _, n := range res.Notes {
		if !n.Changed {
			continue
		}
		changed++
		fmt.Fprintf(b, "  %s rev %d: +%d new, %d superseded, %d confirmed → %s\n",
			n.Topic, n.Revision, n.Added, n.Superseded, n.Confirmed, n.Path)
	}
	if res.Delivered {
		fmt.Fprintf(b, "  briefed overseer (trigger=%s)\n", res.Trigger)
	}
	for _, sk := range res.Skipped {
		fmt.Fprintf(b, "  skipped: %s\n", sk)
	}
	return changed
}

func (s *Server) handleResearchList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.ResearchAgent()
	if agent == nil {
		return mcp.NewToolResultError("research agent not configured"), nil
	}
	notes, err := agent.Store().List()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Research notes (%d):\n", len(notes))
	if len(notes) == 0 {
		b.WriteString("  (none yet — run jevons_research_cycle)\n")
		return mcp.NewToolResultText(b.String()), nil
	}
	for _, n := range notes {
		current := len(n.CurrentFindings())
		fmt.Fprintf(&b, "  %s [%s] revisions=%d current_findings=%d updated=%s\n    %s\n",
			n.ID, n.Topic, len(n.Revisions), current, n.UpdatedAt, agent.Store().NotePath(n.ID))
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleResearchRead(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.ResearchAgent()
	if agent == nil {
		return mcp.NewToolResultError("research agent not configured"), nil
	}
	id := strings.TrimSpace(str(req.GetArguments()["id"]))
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	note, err := agent.Store().Get(id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(research.RenderNote(note)), nil
}

func (s *Server) handleResearchConfigure(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.ResearchAgent()
	if agent == nil {
		return mcp.NewToolResultError("research agent not configured"), nil
	}
	args := req.GetArguments()
	patch := research.ConfigPatch{UpdatedBy: "jevons"}
	if v := strings.TrimSpace(str(args["updated_by"])); v != "" {
		patch.UpdatedBy = v
	}
	for key, dst := range map[string]**bool{
		"enabled":      &patch.Enabled,
		"deliver":      &patch.Deliver,
		"feed_enabled": &patch.FeedEnabled,
	} {
		if v, ok := args[key].(bool); ok {
			*dst = &v
		}
	}
	for key, dst := range map[string]**int{
		"interval_sec":             &patch.IntervalSec,
		"lookback_hours":           &patch.LookbackHours,
		"max_commits":              &patch.MaxCommits,
		"max_repos":                &patch.MaxRepos,
		"max_briefs_per_hour":      &patch.MaxBriefsPerHour,
		"feed_interval_sec":        &patch.FeedIntervalSec,
		"max_feed_items":           &patch.MaxFeedItems,
		"max_feed_cycles_per_hour": &patch.MaxFeedCyclesPerHour,
	} {
		if v, ok := args[key].(float64); ok {
			n := int(v)
			*dst = &n
		}
	}
	if v, ok := args["workdir"].(string); ok {
		w := strings.TrimSpace(v)
		patch.Workdir = &w
	}
	if v, ok := args["overseer"].(string); ok && strings.TrimSpace(v) != "" {
		o := strings.TrimSpace(v)
		patch.Overseer = &o
	}
	if v, ok := args["related_roots"].(string); ok {
		roots := splitCSV(v)
		patch.RelatedRoots = &roots
	}
	if v, ok := args["allowed_hosts"].(string); ok {
		hosts := splitCSV(v)
		patch.AllowedHosts = &hosts
	}
	if name := strings.TrimSpace(str(args["add_feed_name"])); name != "" {
		url := strings.TrimSpace(str(args["add_feed_url"]))
		if url == "" {
			return mcp.NewToolResultError("add_feed_url is required with add_feed_name"), nil
		}
		enabled := true
		if v, ok := args["add_feed_enabled"].(bool); ok {
			enabled = v
		}
		patch.AddFeed = &research.Feed{
			Name:    name,
			URL:     url,
			Topic:   strings.TrimSpace(str(args["add_feed_topic"])),
			Enabled: enabled,
		}
	}
	patch.RemoveFeed = strings.TrimSpace(str(args["remove_feed"]))

	cfg, err := agent.PatchConfig(patch)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("research configure failed: %v", err)), nil
	}
	s.logLifecycle("research", "configure", "ok", map[string]any{
		"updated_by":   cfg.UpdatedBy,
		"enabled":      cfg.Enabled,
		"feed_enabled": cfg.FeedEnabled,
		"feeds":        len(cfg.Feeds),
	})
	return mcp.NewToolResultText(formatResearchConfig(cfg)), nil
}

func (s *Server) handleResearchStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	agent := s.ResearchAgent()
	if agent == nil {
		return mcp.NewToolResultError("research agent not configured"), nil
	}
	cfg, err := agent.Config()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	st, err := agent.State()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	notes, err := agent.Store().List()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var b strings.Builder
	b.WriteString(formatResearchConfig(cfg))
	fmt.Fprintf(&b, "runs=%d last_run=%s last_trigger=%s last_changed=%d\n",
		st.Runs, st.LastRunAt, st.LastTrigger, st.LastChanged)
	fmt.Fprintf(&b, "feed_runs=%d last_feed_run=%s last_feed_items=%d\n",
		st.FeedRuns, st.LastFeedRunAt, st.LastFeedItems)
	fmt.Fprintf(&b, "briefs_sent=%d last_brief=%s last_skip=%s\n",
		st.BriefsSent, st.LastBriefAt, st.LastSkipReason)
	fmt.Fprintf(&b, "notes=%d dir=%s\n", len(notes), agent.Store().Dir())
	if cur, err := research.LoadFeedCursor(agent.Store().Dir()); err == nil {
		for name, m := range cur.Feeds {
			fmt.Fprintf(&b, "  feed %s: polls=%d new_items=%d last_polled=%s last_item=%s err=%s\n",
				name, m.Polls, m.NewItems, m.LastPolledAt, m.LastItemAt, m.LastError)
		}
	}
	return mcp.NewToolResultText(b.String()), nil
}

func formatResearchConfig(cfg research.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Research cycle (🎯T356): enabled=%v interval_sec=%d lookback_hours=%d\n",
		cfg.Enabled, cfg.IntervalSec, cfg.LookbackHours)
	fmt.Fprintf(&b, "  scan: workdir=%q related_roots=%v max_commits=%d max_repos=%d\n",
		cfg.Workdir, cfg.RelatedRoots, cfg.EffectiveMaxCommits(), cfg.EffectiveMaxRepos())
	fmt.Fprintf(&b, "  brief: deliver=%v overseer=%s max_per_hour=%d\n",
		cfg.Deliver, cfg.EffectiveOverseer(), cfg.EffectiveMaxBriefsPerHour())
	fmt.Fprintf(&b, "  feeds: enabled=%v interval_sec=%d max_items=%d max_cycles_per_hour=%d allowed_hosts=%v\n",
		cfg.FeedEnabled, cfg.FeedIntervalSec, cfg.EffectiveMaxFeedItems(),
		cfg.EffectiveMaxFeedCyclesPerHour(), cfg.AllowedHosts)
	for _, f := range cfg.Feeds {
		fmt.Fprintf(&b, "    %s enabled=%v topic=%s %s\n", f.Name, f.Enabled, f.NoteTopic(), f.URL)
	}
	fmt.Fprintf(&b, "  updated_at=%s updated_by=%s\n", cfg.UpdatedAt, cfg.UpdatedBy)
	return b.String()
}

func splitCSV(raw string) []string {
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// overseerBriefDeliverer fire-and-forget posts research briefs to the overseer.
// Uses sendToAgent (queue/interrupt) — never WaitForResponse.
type overseerBriefDeliverer struct {
	server   *Server
	overseer string
	mu       sync.Mutex
}

// NewOverseerBriefDeliverer builds a fire-and-forget research brief deliverer.
func (s *Server) NewOverseerBriefDeliverer(overseer string) research.BriefDeliverer {
	if strings.TrimSpace(overseer) == "" {
		overseer = "jevons"
	}
	return &overseerBriefDeliverer{server: s, overseer: overseer}
}

func (d *overseerBriefDeliverer) DeliverBrief(text string) error {
	if d == nil || d.server == nil {
		return fmt.Errorf("research deliverer: nil server")
	}
	d.mu.Lock()
	target := d.overseer
	d.mu.Unlock()
	// Prefer the live config target (retune-safe).
	if agent := d.server.ResearchAgent(); agent != nil {
		if cfg, err := agent.Config(); err == nil {
			target = cfg.EffectiveOverseer()
		}
	}
	body := butler.FormatEventPush(research.EventSource, text)
	res, err := d.server.sendToAgent(target, body, false)
	if err != nil {
		if d.server.butler != nil {
			if _, err2 := d.server.butler.PushEvent(target, research.EventSource, text); err2 != nil {
				return fmt.Errorf("research deliver send: %w; push: %v", err, err2)
			}
			slog.Info("research brief delivered via push fallback", "target", target)
			return nil
		}
		return err
	}
	slog.Info("research brief delivered", "target", target, "status", res.Status)
	return nil
}
