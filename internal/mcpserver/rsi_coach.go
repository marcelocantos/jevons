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

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/rsi"
)

// SetRSICoach attaches the ambient RSI coach (🎯T243) and registers
// jevons_rsi_coach_* tools. Product path: judgments → overseer; never bullseye.
func (s *Server) SetRSICoach(coach *rsi.Coach) {
	s.mu.Lock()
	s.rsiCoach = coach
	s.mu.Unlock()
	if coach == nil {
		return
	}
	s.registerRSICoachTools()
}

// RSICoach returns the attached coach (may be nil).
func (s *Server) RSICoach() *rsi.Coach {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rsiCoach
}

func (s *Server) registerRSICoachTools() {
	s.addTool(
		mcp.NewTool("jevons_rsi_coach_cycle",
			mcp.WithDescription("Run one RSI coach cycle now: drip (🎯T243) reads new appends since the cursor; retro (🎯T353) makes a bounded pass over history — git commits, eventlog tail, owner chat, session transcripts — within the configured lookback. Coach never files bullseye — overseer alone decides file/alert/brief PO/ignore."),
			mcp.WithBoolean("dry_run", mcp.Description("If true, form judgments and return wire text without delivering to overseer.")),
			mcp.WithString("mode", mcp.Description("drip (default) | retro | both. retro runs the bounded retrospective history pass (🎯T353).")),
		),
		s.handleRSICoachCycle,
	)
	s.addTool(
		mcp.NewTool("jevons_rsi_coach_configure",
			mcp.WithDescription("Retune the RSI coach (🎯T243 bi-directional SI). Overseer updates system prompt and/or runtime dials: enabled, interval_sec, rate_cap, min_count, focus_filters. Alert fatigue is a dial, not a hard veto."),
			mcp.WithBoolean("enabled", mcp.Description("Enable or disable schedule delivery.")),
			mcp.WithNumber("interval_sec", mcp.Description("Schedule period in seconds (0 = leave unchanged; negative disables ticker).")),
			mcp.WithNumber("rate_cap", mcp.Description("Max judgments delivered per cycle (0 = leave unchanged).")),
			mcp.WithNumber("min_count", mcp.Description("Frequency threshold for extract (0 = leave unchanged).")),
			mcp.WithString("system_prompt", mcp.Description("Replace coach system prompt (empty string clears to default).")),
			mcp.WithString("focus_filters", mcp.Description("Comma-separated focus filters (kind/component/message substrings). Empty string clears.")),
			mcp.WithString("overseer", mcp.Description("Deliver target agent name (default jevons).")),
			mcp.WithString("updated_by", mcp.Description("Who is retuning (default: overseer name / jevons).")),
			mcp.WithBoolean("retro_enabled", mcp.Description("🎯T353: enable the scheduled retrospective history pass.")),
			mcp.WithNumber("retro_interval_sec", mcp.Description("🎯T353: retro schedule period in seconds (default 21600 = 6h; negative disables the retro ticker).")),
			mcp.WithNumber("retro_lookback_hours", mcp.Description("🎯T353: how far back one retrospective pass reaches (default 168 = 7d). This bounds the whole pass — history is never re-scanned in full on the drip cadence.")),
			mcp.WithNumber("retro_rate_cap", mcp.Description("🎯T353: max judgments delivered per retrospective pass (default 2).")),
			mcp.WithNumber("retro_min_count", mcp.Description("🎯T353: coarse-conclusion threshold for the retro value bar (default 3).")),
			mcp.WithNumber("retro_max_commits", mcp.Description("🎯T353: max commits mined per pass (default 400).")),
			mcp.WithNumber("retro_max_event_rows", mcp.Description("🎯T353: max eventlog rows scanned per pass (default 2000).")),
			mcp.WithNumber("retro_max_sessions", mcp.Description("🎯T353: max session transcripts scanned per pass (default 12).")),
			mcp.WithString("retro_workdir", mcp.Description("🎯T353: git repository mined for history (empty string clears to the daemon workdir).")),
		),
		s.handleRSICoachConfigure,
	)
	s.addTool(
		mcp.NewTool("jevons_rsi_coach_status",
			mcp.WithDescription("Show RSI coach config, cursor drip offsets, disposition metrics, and system prompt summary (🎯T243/🎯T333)."),
		),
		s.handleRSICoachStatus,
	)
	s.registerRSIDispositionTool()
}

func (s *Server) handleRSICoachCycle(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	coach := s.RSICoach()
	if coach == nil {
		return mcp.NewToolResultError("rsi coach not configured"), nil
	}
	args := req.GetArguments()
	dryHint := false
	if v, ok := args["dry_run"].(bool); ok {
		dryHint = v
	}
	mode := "drip"
	if v, ok := args["mode"].(string); ok && strings.TrimSpace(v) != "" {
		mode = strings.ToLower(strings.TrimSpace(v))
	}
	switch mode {
	case "drip", "retro", "both":
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown mode %q (want drip | retro | both)", mode)), nil
	}

	var res rsi.CoachCycleResult
	if mode == "drip" || mode == "both" {
		r, err := coach.RunOnce("mcp")
		if err != nil {
			s.logLifecycle("rsi_coach", "cycle", "error", map[string]any{"err": err.Error(), "mode": mode})
			return mcp.NewToolResultError(fmt.Sprintf("rsi coach cycle failed: %v", err)), nil
		}
		res = r
	}
	if mode == "retro" || mode == "both" {
		r, err := coach.RunRetroOnce("mcp")
		if err != nil {
			s.logLifecycle("rsi_coach", "retro_cycle", "error", map[string]any{"err": err.Error()})
			return mcp.NewToolResultError(fmt.Sprintf("rsi coach retro cycle failed: %v", err)), nil
		}
		res.Judgments = append(res.Judgments, r.Judgments...)
		res.Delivered = append(res.Delivered, r.Delivered...)
		res.Skipped = append(res.Skipped, r.Skipped...)
		res.WireTexts = append(res.WireTexts, r.WireTexts...)
	}
	s.logLifecycle("rsi_coach", "cycle", "ok", map[string]any{
		"mode":      mode,
		"judgments": len(res.Judgments),
		"delivered": len(res.Delivered),
		"skipped":   len(res.Skipped),
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("RSI coach cycle (mode=%s): judgments=%d delivered=%d skipped=%d\n",
		mode, len(res.Judgments), len(res.Delivered), len(res.Skipped)))
	if mode != "drip" {
		if st, err := coach.RetroState(); err == nil {
			b.WriteString(fmt.Sprintf("  retro window: since=%s commits=%d event_rows=%d chat_turns=%d session_turns=%d\n",
				st.LastWindowStart, st.LastCommits, st.LastEventRows, st.LastChatTurns, st.LastSessionTurns))
		}
	}
	if dryHint && len(res.Delivered) > 0 {
		b.WriteString("(note: dry_run arg is a hint when the coach deliverer is live; use coach DryRun for guaranteed no-deliver)\n")
	}
	for _, j := range res.Delivered {
		b.WriteString(fmt.Sprintf("  delivered: %s (severity=%s fp=%s)\n", j.Observation, j.Severity, j.Fingerprint))
	}
	for i, j := range res.Judgments {
		if i < len(res.Delivered) {
			continue
		}
		b.WriteString(fmt.Sprintf("  judgment: %s (severity=%s fp=%s)\n", j.Observation, j.Severity, j.Fingerprint))
	}
	for _, sk := range res.Skipped {
		b.WriteString(fmt.Sprintf("  skipped: %s (%s)\n", sk.Name, sk.Reason))
	}
	// Include wire sample for overseer inspection (first only).
	if len(res.WireTexts) > 0 {
		b.WriteString("\n--- wire sample ---\n")
		b.WriteString(res.WireTexts[0])
		b.WriteByte('\n')
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleRSICoachConfigure(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	coach := s.RSICoach()
	if coach == nil {
		return mcp.NewToolResultError("rsi coach not configured"), nil
	}
	args := req.GetArguments()
	patch := rsi.CoachConfigPatch{UpdatedBy: "jevons"}
	if v, ok := args["updated_by"].(string); ok && strings.TrimSpace(v) != "" {
		patch.UpdatedBy = strings.TrimSpace(v)
	}
	if v, ok := args["enabled"].(bool); ok {
		patch.Enabled = &v
	}
	if v, ok := args["interval_sec"].(float64); ok {
		n := int(v)
		patch.IntervalSec = &n
	}
	if v, ok := args["rate_cap"].(float64); ok {
		n := int(v)
		patch.RateCap = &n
	}
	if v, ok := args["min_count"].(float64); ok {
		n := int(v)
		patch.MinCount = &n
	}
	if v, ok := args["system_prompt"].(string); ok {
		// Empty string clears override (back to default).
		patch.SystemPrompt = &v
	}
	if v, ok := args["focus_filters"].(string); ok {
		var filters []string
		if strings.TrimSpace(v) != "" {
			for _, p := range strings.Split(v, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					filters = append(filters, p)
				}
			}
		}
		patch.FocusFilters = &filters
	}
	if v, ok := args["overseer"].(string); ok && strings.TrimSpace(v) != "" {
		o := strings.TrimSpace(v)
		patch.Overseer = &o
	}
	// Retrospective dials (🎯T353).
	if v, ok := args["retro_enabled"].(bool); ok {
		patch.RetroEnabled = &v
	}
	for key, dst := range map[string]**int{
		"retro_interval_sec":   &patch.RetroIntervalSec,
		"retro_lookback_hours": &patch.RetroLookbackHours,
		"retro_rate_cap":       &patch.RetroRateCap,
		"retro_min_count":      &patch.RetroMinCount,
		"retro_max_commits":    &patch.RetroMaxCommits,
		"retro_max_event_rows": &patch.RetroMaxEventRows,
		"retro_max_sessions":   &patch.RetroMaxSessions,
	} {
		if v, ok := args[key].(float64); ok {
			n := int(v)
			*dst = &n
		}
	}
	if v, ok := args["retro_workdir"].(string); ok {
		w := strings.TrimSpace(v)
		patch.RetroWorkdir = &w
	}

	cfg, err := coach.PatchConfig(patch)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("rsi coach configure failed: %v", err)), nil
	}
	s.logLifecycle("rsi_coach", "configure", "ok", map[string]any{
		"updated_by": cfg.UpdatedBy,
		"enabled":    cfg.Enabled,
		"rate_cap":   cfg.RateCap,
	})
	return mcp.NewToolResultText(fmt.Sprintf(
		"RSI coach retuned: enabled=%v interval_sec=%d rate_cap=%d min_count=%d focus=%v overseer=%s updated_by=%s\n"+
			"retro (🎯T353): enabled=%v interval_sec=%d lookback_hours=%d rate_cap=%d min_count=%d max_commits=%d workdir=%q\n"+
			"System prompt length=%d (effective uses override when non-empty).",
		cfg.Enabled, cfg.IntervalSec, cfg.EffectiveRateCap(), cfg.EffectiveMinCount(),
		cfg.FocusFilters, cfg.EffectiveOverseer(), cfg.UpdatedBy,
		cfg.RetroEnabled, cfg.RetroIntervalSec, int(cfg.RetroLookback()/time.Hour),
		cfg.EffectiveRetroRateCap(), cfg.EffectiveRetroMinCount(), cfg.RetroMaxCommits, cfg.RetroWorkdir,
		len(cfg.SystemPrompt),
	)), nil
}

func (s *Server) handleRSICoachStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	coach := s.RSICoach()
	if coach == nil {
		return mcp.NewToolResultError("rsi coach not configured"), nil
	}
	cfg, err := coach.Config()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cur, err := rsi.LoadCoachCursor(coach.StateDir())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	prompt := cfg.EffectiveSystemPrompt()
	preview := prompt
	if len(preview) > 240 {
		preview = preview[:240] + "…"
	}
	dispLine := "  dispositions: unavailable\n"
	if store := coach.Dispositions(); store != nil {
		if m, err := store.Metrics(time.Now().UTC(), 14*24*time.Hour); err == nil {
			dispLine = fmt.Sprintf(
				"  dispositions(14d, 🎯T333): total=%d pending=%d filed=%d parked=%d ignored=%d act_other=%d achieved=%d set_aside=%d\n",
				m.Total, m.Pending, m.Filed, m.Parked, m.Ignored, m.ActOther, m.Achieved, m.SetAside)
		} else {
			dispLine = fmt.Sprintf("  dispositions: error: %v\n", err)
		}
	}
	retroLine := "  retro(🎯T353): state unavailable\n"
	if st, err := coach.RetroState(); err == nil {
		retroLine = fmt.Sprintf(
			"  retro(🎯T353): enabled=%v interval_sec=%d lookback_hours=%d rate_cap=%d min_count=%d workdir=%q\n"+
				"    last_run=%s window_start=%s runs=%d commits=%d event_rows=%d chat_turns=%d session_turns=%d judgments=%d delivered=%d suppressed=%d\n",
			cfg.RetroEnabled, cfg.RetroIntervalSec, int(cfg.RetroLookback()/time.Hour),
			cfg.EffectiveRetroRateCap(), cfg.EffectiveRetroMinCount(), cfg.RetroWorkdir,
			st.LastRunAt, st.LastWindowStart, st.Runs, st.LastCommits, st.LastEventRows,
			st.LastChatTurns, st.LastSessionTurns, st.LastJudgments, st.LastDelivered, st.LastSuppressed,
		)
	}
	text := fmt.Sprintf(
		"RSI coach status (🎯T243)\n  name=%s overseer=%s enabled=%v\n  interval_sec=%d rate_cap=%d min_count=%d\n  focus_filters=%v\n  updated_at=%s updated_by=%s\n  cursor: chat_byte=%d event_byte=%d sessions=%d\n%s%s  system_prompt_preview:\n%s\n",
		cfg.EffectiveCoachName(), cfg.EffectiveOverseer(), cfg.Enabled,
		cfg.IntervalSec, cfg.EffectiveRateCap(), cfg.EffectiveMinCount(),
		cfg.FocusFilters, cfg.UpdatedAt, cfg.UpdatedBy,
		cur.ChatLogByte, cur.EventLogByte, len(cur.SessionOffsets),
		dispLine,
		retroLine,
		preview,
	)
	return mcp.NewToolResultText(text), nil
}

// overseerJudgmentDeliverer fire-and-forget posts coach judgments to the overseer.
// Uses sendToAgent (queue/interrupt) — never WaitForResponse (idle_nudge lesson).
type overseerJudgmentDeliverer struct {
	server   *Server
	overseer string
	mu       sync.Mutex
}

// NewOverseerJudgmentDeliverer builds a fire-and-forget deliverer to the overseer.
func (s *Server) NewOverseerJudgmentDeliverer(overseer string) rsi.JudgmentDeliverer {
	if strings.TrimSpace(overseer) == "" {
		overseer = "jevons"
	}
	return &overseerJudgmentDeliverer{server: s, overseer: overseer}
}

func (d *overseerJudgmentDeliverer) DeliverJudgment(text string) error {
	if d == nil || d.server == nil {
		return fmt.Errorf("rsi coach deliverer: nil server")
	}
	d.mu.Lock()
	target := d.overseer
	d.mu.Unlock()
	// Prefer live config overseer (retune-safe).
	if coach := d.server.RSICoach(); coach != nil {
		if cfg, err := coach.Config(); err == nil {
			target = cfg.EffectiveOverseer()
		}
	}
	body := butler.FormatEventPush(rsi.CoachEventSource, text)
	// Fire-and-forget: interrupt=false queues if busy.
	res, err := d.server.sendToAgent(target, body, false)
	if err != nil {
		// Fallback: if registry/send unavailable, try butler PushEvent when present
		// (may block — last resort).
		if d.server.butler != nil {
			_, err2 := d.server.butler.PushEvent(target, rsi.CoachEventSource, text)
			if err2 != nil {
				return fmt.Errorf("coach deliver send: %w; push: %v", err, err2)
			}
			slog.Info("rsi coach delivered via push fallback", "target", target)
			return nil
		}
		return err
	}
	slog.Info("rsi coach delivered judgment", "target", target, "status", res.Status)
	return nil
}
