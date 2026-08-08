// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/rsi"
)

// registerRSIDispositionTool exposes the 🎯T333 judgment-disposition loop:
// record what the overseer decided for each coach judgment, list the ledger,
// and report rates so mute spam / zero-disposition are visible.
func (s *Server) registerRSIDispositionTool() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_rsi_disposition",
			mcp.WithDescription("RSI coach judgment dispositions (🎯T333). action=record stores the overseer's decision for a judgment fingerprint (file|park|ignore_with_reason|act_other; file needs target_id — prefer jevons_target_file with fingerprint=, which records automatically). action=list shows the ledger; action=metrics reports filed/parked/ignored/pending rates over a window. Outcomes (achieve/set_aside) sync automatically and suppress re-propose without new evidence."),
			mcp.WithString("action", mcp.Description("record | list | metrics (default list)")),
			mcp.WithString("fingerprint", mcp.Description("Judgment fingerprint (from the coach wire text). Required for record.")),
			mcp.WithString("disposition", mcp.Description("record: file | park | ignore_with_reason | act_other")),
			mcp.WithString("reason", mcp.Description("record: why (required for ignore_with_reason)")),
			mcp.WithString("target_id", mcp.Description("record disposition=file: the filed 🎯 id")),
			mcp.WithString("cwd", mcp.Description("record disposition=file: repo the target was filed in (outcome sync)")),
			mcp.WithString("evidence", mcp.Description("record: evidence pointer (session/event id)")),
			mcp.WithNumber("window_days", mcp.Description("metrics: window in days (default 14)")),
		),
		s.handleRSIDisposition,
	)
}

func (s *Server) handleRSIDisposition(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	coach := s.RSICoach()
	if coach == nil {
		return mcp.NewToolResultError("rsi coach not configured"), nil
	}
	store := coach.Dispositions()
	if store == nil {
		return mcp.NewToolResultError("rsi disposition store not available"), nil
	}
	args := req.GetArguments()
	action := strings.TrimSpace(str(args["action"]))
	if action == "" {
		action = "list"
	}
	now := time.Now().UTC()

	switch action {
	case "record":
		entry, err := store.SetDisposition(rsi.SetDispositionArgs{
			Fingerprint: str(args["fingerprint"]),
			Disposition: str(args["disposition"]),
			Reason:      str(args["reason"]),
			TargetID:    str(args["target_id"]),
			TargetCwd:   str(args["cwd"]),
			Evidence:    str(args["evidence"]),
			Now:         now,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("rsi disposition record failed: %v", err)), nil
		}
		s.logLifecycle("rsi_disposition", "record", "ok", map[string]any{
			"fingerprint": entry.Fingerprint,
			"disposition": entry.Disposition,
			"target_id":   entry.TargetID,
		})
		txt := fmt.Sprintf("Disposition recorded: fp=%s disposition=%s", entry.Fingerprint, entry.Disposition)
		if entry.TargetID != "" {
			txt += " target=🎯" + entry.TargetID
		}
		if entry.Reason != "" {
			txt += " reason=" + entry.Reason
		}
		return mcp.NewToolResultText(txt + "\n"), nil

	case "metrics":
		window := 14 * 24 * time.Hour
		if v, ok := args["window_days"].(float64); ok && v > 0 {
			window = time.Duration(v) * 24 * time.Hour
		}
		m, err := store.Metrics(now, window)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("rsi disposition metrics failed: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"RSI disposition metrics (🎯T333, window=%dd)\n  total=%d pending=%d filed=%d parked=%d ignored=%d act_other=%d\n  outcomes: achieved=%d set_aside=%d\n",
			int(m.Window.Hours()/24), m.Total, m.Pending, m.Filed, m.Parked, m.Ignored, m.ActOther,
			m.Achieved, m.SetAside,
		)), nil

	case "list":
		entries, err := store.Entries()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("rsi disposition list failed: %v", err)), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("RSI dispositions (🎯T333): %d entries\n", len(entries)))
		const maxList = 50
		for i, e := range entries {
			if i >= maxList {
				fmt.Fprintf(&b, "  … %d more\n", len(entries)-maxList)
				break
			}
			line := fmt.Sprintf("  fp=%s disposition=%s delivered=%s",
				e.Fingerprint, e.Disposition, e.DeliveredAt.Format(time.RFC3339))
			if e.TargetID != "" {
				line += " target=🎯" + e.TargetID
			}
			if e.Outcome != "" {
				line += " outcome=" + e.Outcome
			}
			if e.Reason != "" {
				line += fmt.Sprintf(" reason=%q", e.Reason)
			}
			if n := strings.TrimSpace(e.Name); n != "" {
				line += fmt.Sprintf(" name=%q", n)
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
		return mcp.NewToolResultText(b.String()), nil

	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown action %q (record|list|metrics)", action)), nil
	}
}
