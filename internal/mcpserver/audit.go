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

	"github.com/marcelocantos/jevons/internal/audit"
	"github.com/marcelocantos/jevons/internal/butler"
)

// SetAuditor attaches the periodic full-scan auditor (🎯T357) and registers
// the jevons_audit_* tools. The auditor suggests; the overseer disposes —
// there is deliberately no tool here that files a bullseye target, so a
// finding becomes tracked work only through an overseer decision.
func (s *Server) SetAuditor(auditor *audit.Auditor) {
	s.mu.Lock()
	s.auditor = auditor
	s.mu.Unlock()
	if auditor == nil {
		return
	}
	s.registerAuditTools()
}

// Auditor returns the attached auditor (may be nil).
func (s *Server) Auditor() *audit.Auditor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auditor
}

func (s *Server) registerAuditTools() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_audit_cycle",
			mcp.WithDescription("Run one bounded full-scan audit now (🎯T357): scope product code, skills trees, and agent prompts, hand the manifest to the advanced-tier auditor, and fold the findings into durable residue. A pass is bounded by scope caps, a wall-clock timeout, and a cycles-per-day cost guard. Findings land as a durable report plus residue — new and reopened criticals notify the overseer in the same cycle."),
			mcp.WithBoolean("force", mcp.Description("Bypass the min-gap and cycles-per-day cost guards for this run.")),
			mcp.WithString("reason", mcp.Description("Trigger label recorded on the report (default mcp).")),
		),
		s.handleAuditCycle,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_audit_status",
			mcp.WithDescription("Show the audit cycle's config (model pin, scope roots, bounds), its run record, and the outstanding residue by severity (🎯T357)."),
		),
		s.handleAuditStatus,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_audit_residue",
			mcp.WithDescription("Read the durable audit residue (🎯T357), or record the overseer's handling of one finding. With no fingerprint: list outstanding findings, newest activity last. With fingerprint + disposition: mark it filed (with target_id), ignore_with_reason (reason required), accepted, or pending. Residue is the memory that turns repeated passes into a converging list rather than re-reported noise."),
			mcp.WithString("fingerprint", mcp.Description("Finding fingerprint from a notice or listing. Omit to list.")),
			mcp.WithString("disposition", mcp.Description("filed | ignore_with_reason | accepted | pending")),
			mcp.WithString("reason", mcp.Description("Why — required for ignore_with_reason.")),
			mcp.WithString("target_id", mcp.Description("Bullseye target the finding was filed as (e.g. T361).")),
			mcp.WithBoolean("all", mcp.Description("Include resolved entries in the listing.")),
		),
		s.handleAuditResidue,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_audit_report",
			mcp.WithDescription("Read one durable audit report artifact (🎯T357): the findings a single pass produced, with the model, the scopes covered, and any scope truncation. Omit id for the most recent report."),
			mcp.WithString("id", mcp.Description("Report id (e.g. 20260809T031500Z). Omit for the latest.")),
		),
		s.handleAuditReport,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_audit_configure",
			mcp.WithDescription("Retune the periodic full-scan auditor (🎯T357): cadence, the advanced-tier model pin, scope roots, pass bounds, and notification policy. The model pin is durable config, not a hardcoded call site — audits deliberately run on an advanced tier."),
			mcp.WithBoolean("enabled", mcp.Description("Enable or disable the standing schedule (manual cycles still run).")),
			mcp.WithNumber("interval_sec", mcp.Description("Full-scan cadence in seconds (default 86400; negative disables the ticker).")),
			mcp.WithString("provider", mcp.Description("Auditor backend (default claude).")),
			mcp.WithString("model", mcp.Description("Advanced-tier model pin (default claude-fable-5).")),
			mcp.WithString("command", mcp.Description("Comma-separated auditor invocation; {model} is substituted.")),
			mcp.WithString("workdir", mcp.Description("Repository the auditor runs in.")),
			mcp.WithString("code_roots", mcp.Description("Comma-separated product-code roots. Empty string clears.")),
			mcp.WithString("skills_roots", mcp.Description("Comma-separated skills-tree roots. Empty string clears.")),
			mcp.WithString("prompt_roots", mcp.Description("Comma-separated prompt/persona/brief roots. Empty string clears.")),
			mcp.WithNumber("timeout_sec", mcp.Description("Wall-clock bound on one pass (default 1200).")),
			mcp.WithNumber("max_files_per_scope", mcp.Description("Files listed per scope per pass (default 400).")),
			mcp.WithNumber("max_bytes_per_scope", mcp.Description("Manifest bytes per scope per pass (default 4 MiB).")),
			mcp.WithNumber("max_file_bytes", mcp.Description("Skip individual files larger than this (default 256 KiB).")),
			mcp.WithNumber("max_findings", mcp.Description("Findings accepted from one report (default 40).")),
			mcp.WithNumber("max_output_bytes", mcp.Description("Bytes read back from the auditor (default 1 MiB).")),
			mcp.WithNumber("max_cycles_per_day", mcp.Description("Standing cost guard on scheduled passes (default 4).")),
			mcp.WithNumber("min_cycle_gap_sec", mcp.Description("Anti-thrash floor between cycles (default 300).")),
			mcp.WithNumber("keep_reports", mcp.Description("Report artifacts retained on disk (default 30).")),
			mcp.WithBoolean("deliver", mcp.Description("Notify the overseer in-cycle on qualifying findings.")),
			mcp.WithString("overseer", mcp.Description("Notification target agent (default jevons).")),
			mcp.WithString("notify_severity", mcp.Description("Minimum severity that notifies: info | low | medium | high | critical (default critical).")),
			mcp.WithNumber("max_notifies_per_cycle", mcp.Description("Cap on in-cycle notifications (default 3).")),
			mcp.WithString("system_prompt", mcp.Description("Override the standing auditor brief. Empty string restores the default.")),
			mcp.WithString("updated_by", mcp.Description("Who is retuning (default jevons).")),
		),
		s.handleAuditConfigure,
	)
}

func (s *Server) handleAuditCycle(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	auditor := s.Auditor()
	if auditor == nil {
		return mcp.NewToolResultError("auditor not configured"), nil
	}
	args := req.GetArguments()
	force, _ := args["force"].(bool)
	reason := strings.TrimSpace(str(args["reason"]))
	if reason == "" {
		reason = "mcp"
	}

	res, err := auditor.RunOnce(ctx, reason, force)
	if err != nil {
		s.logLifecycle("audit", "cycle", "error", map[string]any{"err": err.Error(), "reason": reason})
		// A failed pass is still durable evidence: report the artifact id so
		// a broken auditor cannot read as "nothing to report".
		if res.ReportID != "" {
			return mcp.NewToolResultError(fmt.Sprintf("audit cycle failed: %v (report %s)", err, res.ReportID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("audit cycle failed: %v", err)), nil
	}
	s.logLifecycle("audit", "cycle", "ok", map[string]any{
		"reason":   reason,
		"report":   res.ReportID,
		"findings": len(res.Report.Findings),
		"open":     res.Merge.OpenTotal,
		"notified": res.Notified,
	})
	return mcp.NewToolResultText(formatAuditCycle(res)), nil
}

func formatAuditCycle(res audit.CycleResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Audit cycle (🎯T357) reason=%s\n", res.Reason)
	if res.Skipped != "" {
		fmt.Fprintf(&b, "  skipped: %s\n", res.Skipped)
		return b.String()
	}
	fmt.Fprintf(&b, "  scope: %d files, full_scan=%v covered=%s\n",
		res.FilesScanned, res.FullScan, joinAuditScopes(res.CoveredScopes))
	if len(res.MissingScopes) > 0 {
		fmt.Fprintf(&b, "  PARTIAL: no files in scope for %s — those surfaces were not audited\n",
			joinAuditScopes(res.MissingScopes))
	}
	if res.Dry {
		b.WriteString("  dry run: no auditor wired, manifest only (residue untouched)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  auditor: %s/%s tier=%s took=%s\n",
		res.Provider, res.Model, res.Tier, res.Duration.Round(1e9))
	fmt.Fprintf(&b, "  report %s: %d findings", res.ReportID, len(res.Report.Findings))
	if res.Report.Dropped > 0 {
		fmt.Fprintf(&b, " (%d dropped over the cap)", res.Report.Dropped)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  residue: new=%d reopened=%d resolved=%d updated=%d open_total=%d\n",
		len(res.Merge.New), len(res.Merge.Reopened), len(res.Merge.Resolved),
		len(res.Merge.Updated), res.Merge.OpenTotal)
	if res.Notified > 0 {
		fmt.Fprintf(&b, "  notified overseer: %d finding(s) at or above threshold\n", res.Notified)
	}
	for _, e := range res.Merge.Notify {
		fmt.Fprintf(&b, "  - [%s] %s (%s) %s\n",
			strings.ToUpper(string(e.Severity)), e.Title, e.Scope, e.Fingerprint)
	}
	if res.Report.Summary != "" {
		fmt.Fprintf(&b, "  summary: %s\n", res.Report.Summary)
	}
	return b.String()
}

func (s *Server) handleAuditStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	auditor := s.Auditor()
	if auditor == nil {
		return mcp.NewToolResultError("auditor not configured"), nil
	}
	cfg, err := auditor.Config()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	st, err := auditor.State()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	open, err := auditor.Residue().OpenEntries()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var b strings.Builder
	b.WriteString(formatAuditConfig(cfg))
	fmt.Fprintf(&b, "cycles=%d last_cycle=%s last_reason=%s last_report=%s\n",
		st.CycleCount, formatAuditTime(st.LastCycleAt), st.LastReason, st.LastReportID)
	fmt.Fprintf(&b, "last_findings=%d last_notified=%d last_model=%s last_tier=%s last_duration=%.1fs\n",
		st.LastFindings, st.LastNotified, st.LastModel, st.LastTier, st.LastDurationS)
	if st.LastError != "" {
		fmt.Fprintf(&b, "last_error: %s\n", st.LastError)
	}
	if st.LastSkipReason != "" {
		fmt.Fprintf(&b, "last_skip: %s\n", st.LastSkipReason)
	}

	bySeverity := map[audit.Severity]int{}
	undisposed := 0
	for _, e := range open {
		bySeverity[e.Severity]++
		if e.Disposition == "" || e.Disposition == audit.DispositionPending {
			undisposed++
		}
	}
	fmt.Fprintf(&b, "residue: open=%d undisposed=%d critical=%d high=%d medium=%d low=%d info=%d\n",
		len(open), undisposed,
		bySeverity[audit.SeverityCritical], bySeverity[audit.SeverityHigh],
		bySeverity[audit.SeverityMedium], bySeverity[audit.SeverityLow],
		bySeverity[audit.SeverityInfo])
	fmt.Fprintf(&b, "  ledger=%s\n", auditor.Residue().Path())
	if ids, err := audit.ListReportIDs(auditor.StateDir()); err == nil {
		fmt.Fprintf(&b, "  reports=%d dir=%s\n", len(ids), audit.ReportsDir(auditor.StateDir()))
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleAuditResidue(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	auditor := s.Auditor()
	if auditor == nil {
		return mcp.NewToolResultError("auditor not configured"), nil
	}
	args := req.GetArguments()
	fingerprint := strings.TrimSpace(str(args["fingerprint"]))
	disposition := strings.TrimSpace(str(args["disposition"]))

	if fingerprint != "" || disposition != "" {
		if fingerprint == "" {
			return mcp.NewToolResultError("fingerprint is required with disposition"), nil
		}
		if disposition == "" {
			return mcp.NewToolResultError("disposition is required with fingerprint"), nil
		}
		entry, err := auditor.Residue().SetDisposition(
			fingerprint, disposition,
			strings.TrimSpace(str(args["reason"])),
			strings.TrimSpace(str(args["target_id"])),
			auditor.Now())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		s.logLifecycle("audit", "disposition", "ok", map[string]any{
			"fingerprint": entry.Fingerprint,
			"disposition": entry.Disposition,
			"target":      entry.TargetID,
		})
		var b strings.Builder
		fmt.Fprintf(&b, "Disposition recorded (🎯T357): %s → %s\n", entry.Fingerprint, entry.Disposition)
		writeAuditEntry(&b, entry)
		return mcp.NewToolResultText(b.String()), nil
	}

	all, _ := args["all"].(bool)
	var entries []audit.ResidueEntry
	var err error
	if all {
		entries, err = auditor.Residue().Entries()
	} else {
		entries, err = auditor.Residue().OpenEntries()
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var b strings.Builder
	scope := "open"
	if all {
		scope = "all"
	}
	fmt.Fprintf(&b, "Audit residue (%s, %d):\n", scope, len(entries))
	if len(entries) == 0 {
		b.WriteString("  (none — run jevons_audit_cycle)\n")
		return mcp.NewToolResultText(b.String()), nil
	}
	for _, e := range entries {
		writeAuditEntry(&b, e)
	}
	return mcp.NewToolResultText(b.String()), nil
}

func writeAuditEntry(b *strings.Builder, e audit.ResidueEntry) {
	fmt.Fprintf(b, "- [%s] %s (%s, %s x%d)\n",
		strings.ToUpper(string(e.Severity)), e.Title, e.Scope, e.Status, e.SeenCount)
	if e.Path != "" {
		fmt.Fprintf(b, "    at: %s", e.Path)
		if e.Line > 0 {
			fmt.Fprintf(b, ":%d", e.Line)
		}
		b.WriteString("\n")
	}
	if e.MaxSeverity != "" && e.MaxSeverity != e.Severity {
		fmt.Fprintf(b, "    once rated: %s\n", e.MaxSeverity)
	}
	if e.Reopens > 0 {
		fmt.Fprintf(b, "    reopened: x%d\n", e.Reopens)
	}
	disp := e.Disposition
	if disp == "" {
		disp = audit.DispositionPending
	}
	fmt.Fprintf(b, "    disposition: %s", disp)
	if e.TargetID != "" {
		fmt.Fprintf(b, " → %s", e.TargetID)
	}
	if e.Reason != "" {
		fmt.Fprintf(b, " (%s)", e.Reason)
	}
	b.WriteString("\n")
	if e.SuggestedTarget != nil {
		fmt.Fprintf(b, "    suggested target: %s\n", e.SuggestedTarget.Name)
	}
	fmt.Fprintf(b, "    fingerprint: %s\n", e.Fingerprint)
}

func (s *Server) handleAuditReport(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	auditor := s.Auditor()
	if auditor == nil {
		return mcp.NewToolResultError("auditor not configured"), nil
	}
	id := strings.TrimSpace(str(req.GetArguments()["id"]))
	if id == "" {
		ids, err := audit.ListReportIDs(auditor.StateDir())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(ids) == 0 {
			return mcp.NewToolResultText("No audit reports yet — run jevons_audit_cycle.\n"), nil
		}
		id = ids[len(ids)-1]
	}
	rep, err := audit.LoadReport(auditor.StateDir(), id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Audit report %s (🎯T357)\n", rep.ID)
	fmt.Fprintf(&b, "  reason=%s model=%s/%s started=%s\n",
		rep.Reason, rep.Provider, rep.Model, formatAuditTime(rep.StartedAt))
	fmt.Fprintf(&b, "  scopes=%s files=%d\n", joinAuditScopes(rep.Scopes), rep.FilesInMan)
	if len(rep.MissingScopes) > 0 {
		fmt.Fprintf(&b, "  PARTIAL: no %s in scope\n", joinAuditScopes(rep.MissingScopes))
	}
	if rep.Truncated {
		b.WriteString("  NOTE: scope truncated by pass bounds — surfaces are not fully covered.\n")
	}
	if rep.Error != "" {
		fmt.Fprintf(&b, "  ERROR: %s\n", rep.Error)
	}
	if rep.Summary != "" {
		fmt.Fprintf(&b, "  summary: %s\n", rep.Summary)
	}
	fmt.Fprintf(&b, "  findings: %d\n", len(rep.Findings))
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "  - [%s] %s (%s)\n", strings.ToUpper(string(f.Severity)), f.Title, f.Scope)
		if f.Path != "" {
			fmt.Fprintf(&b, "      at: %s", f.Path)
			if f.Line > 0 {
				fmt.Fprintf(&b, ":%d", f.Line)
			}
			b.WriteString("\n")
		}
		if f.Detail != "" {
			fmt.Fprintf(&b, "      %s\n", f.Detail)
		}
		if f.Evidence != "" {
			fmt.Fprintf(&b, "      evidence: %s\n", f.Evidence)
		}
	}
	fmt.Fprintf(&b, "  path: %s\n", audit.ReportPath(auditor.StateDir(), rep.ID))
	return mcp.NewToolResultText(b.String()), nil
}

func (s *Server) handleAuditConfigure(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	auditor := s.Auditor()
	if auditor == nil {
		return mcp.NewToolResultError("auditor not configured"), nil
	}
	args := req.GetArguments()
	patch := audit.ConfigPatch{UpdatedBy: "jevons"}
	if v := strings.TrimSpace(str(args["updated_by"])); v != "" {
		patch.UpdatedBy = v
	}
	for key, dst := range map[string]**bool{
		"enabled": &patch.Enabled,
		"deliver": &patch.Deliver,
	} {
		if v, ok := args[key].(bool); ok {
			*dst = &v
		}
	}
	for key, dst := range map[string]**int{
		"interval_sec":           &patch.IntervalSec,
		"timeout_sec":            &patch.TimeoutSec,
		"max_files_per_scope":    &patch.MaxFilesPerScope,
		"max_bytes_per_scope":    &patch.MaxBytesPerScope,
		"max_file_bytes":         &patch.MaxFileBytes,
		"max_findings":           &patch.MaxFindings,
		"max_output_bytes":       &patch.MaxOutputBytes,
		"max_cycles_per_day":     &patch.MaxCyclesPerDay,
		"min_cycle_gap_sec":      &patch.MinCycleGapSec,
		"keep_reports":           &patch.KeepReports,
		"max_notifies_per_cycle": &patch.MaxNotifiesPerCycle,
	} {
		if v, ok := args[key].(float64); ok {
			n := int(v)
			*dst = &n
		}
	}
	// Strings that may be cleared use presence, not non-emptiness, so an
	// explicit "" is a clear rather than a silent no-op.
	for key, dst := range map[string]**string{
		"workdir":       &patch.Workdir,
		"system_prompt": &patch.SystemPrompt,
	} {
		if v, ok := args[key].(string); ok {
			val := strings.TrimSpace(v)
			*dst = &val
		}
	}
	for key, dst := range map[string]**string{
		"provider":        &patch.Provider,
		"model":           &patch.Model,
		"overseer":        &patch.Overseer,
		"notify_severity": &patch.NotifySeverity,
	} {
		if v, ok := args[key].(string); ok && strings.TrimSpace(v) != "" {
			val := strings.TrimSpace(v)
			*dst = &val
		}
	}
	for key, dst := range map[string]**[]string{
		"code_roots":   &patch.CodeRoots,
		"skills_roots": &patch.SkillsRoots,
		"prompt_roots": &patch.PromptRoots,
		"command":      &patch.Command,
	} {
		if v, ok := args[key].(string); ok {
			list := splitCSV(v)
			*dst = &list
		}
	}
	if sev := strings.TrimSpace(str(args["notify_severity"])); sev != "" && audit.NormalizeSeverity(sev) == "" {
		return mcp.NewToolResultError(fmt.Sprintf("unknown notify_severity %q (want info | low | medium | high | critical)", sev)), nil
	}

	cfg, err := auditor.PatchConfig(patch)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("audit configure failed: %v", err)), nil
	}
	s.logLifecycle("audit", "configure", "ok", map[string]any{
		"updated_by": cfg.UpdatedBy,
		"enabled":    cfg.Enabled,
		"model":      cfg.EffectiveModel(),
		"deliver":    cfg.Deliver,
	})
	return mcp.NewToolResultText(formatAuditConfig(cfg)), nil
}

func formatAuditConfig(cfg audit.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Full-scan audit (🎯T357): enabled=%v interval_sec=%d\n", cfg.Enabled, cfg.IntervalSec)
	fmt.Fprintf(&b, "  auditor: %s/%s (advanced-tier pin) command=%v\n",
		cfg.EffectiveProvider(), cfg.EffectiveModel(), cfg.EffectiveCommand())
	fmt.Fprintf(&b, "  workdir: %s\n", cfg.Workdir)
	fmt.Fprintf(&b, "  code_roots: %v\n", cfg.CodeRoots)
	fmt.Fprintf(&b, "  skills_roots: %v\n", cfg.SkillsRoots)
	fmt.Fprintf(&b, "  prompt_roots: %v\n", cfg.PromptRoots)
	bounds := cfg.Bounds()
	fmt.Fprintf(&b, "  bounds: timeout=%s max_files_per_scope=%d max_bytes_per_scope=%d max_file_bytes=%d\n",
		cfg.Timeout(), bounds.MaxFilesPerScope, bounds.MaxBytesPerScope, bounds.MaxFileBytes)
	fmt.Fprintf(&b, "  cost: max_cycles_per_day=%d min_cycle_gap=%s max_findings=%d max_output_bytes=%d keep_reports=%d\n",
		cfg.EffectiveMaxCyclesPerDay(), cfg.MinCycleGap(), cfg.EffectiveMaxFindings(),
		cfg.EffectiveMaxOutputBytes(), cfg.EffectiveKeepReports())
	fmt.Fprintf(&b, "  notify: deliver=%v overseer=%s at_or_above=%s max_per_cycle=%d\n",
		cfg.Deliver, cfg.EffectiveOverseer(), cfg.EffectiveNotifySeverity(), cfg.EffectiveMaxNotifies())
	if cfg.SystemPrompt != "" {
		fmt.Fprintf(&b, "  system_prompt: overridden (%d bytes)\n", len(cfg.SystemPrompt))
	}
	fmt.Fprintf(&b, "  updated_at=%s updated_by=%s\n", cfg.UpdatedAt, cfg.UpdatedBy)
	return b.String()
}

func joinAuditScopes(kinds []audit.ScopeKind) string {
	if len(kinds) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, ",")
}

func formatAuditTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format(time.RFC3339)
}

// overseerAuditDeliverer fire-and-forget posts audit notices to the overseer.
// Uses sendToAgent (queue/interrupt) — never WaitForResponse: an audit notice
// is news, not a request, and must never block the cycle on a busy seat.
type overseerAuditDeliverer struct {
	server   *Server
	overseer string
	mu       sync.Mutex
}

// NewOverseerAuditDeliverer builds a fire-and-forget audit notice deliverer.
func (s *Server) NewOverseerAuditDeliverer(overseer string) audit.Deliverer {
	if strings.TrimSpace(overseer) == "" {
		overseer = audit.DefaultOverseer
	}
	return &overseerAuditDeliverer{server: s, overseer: overseer}
}

func (d *overseerAuditDeliverer) DeliverAudit(text string) error {
	if d == nil || d.server == nil {
		return fmt.Errorf("audit deliverer: nil server")
	}
	d.mu.Lock()
	target := d.overseer
	d.mu.Unlock()
	// Prefer the live config target (retune-safe).
	if auditor := d.server.Auditor(); auditor != nil {
		if cfg, err := auditor.Config(); err == nil {
			target = cfg.EffectiveOverseer()
		}
	}
	body := butler.FormatEventPush(audit.EventSource, text)
	res, err := d.server.sendToAgent(target, body, false)
	if err != nil {
		if d.server.butler != nil {
			if _, err2 := d.server.butler.PushEvent(target, audit.EventSource, text); err2 != nil {
				return fmt.Errorf("audit deliver send: %w; push: %v", err, err2)
			}
			slog.Info("audit notice delivered via push fallback", "target", target)
			return nil
		}
		return err
	}
	slog.Info("audit notice delivered", "target", target, "status", res.Status)
	return nil
}
