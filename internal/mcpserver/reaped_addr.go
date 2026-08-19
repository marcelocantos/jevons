// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// StatusReapedHeld is the send outcome when the destination was auto-reaped
// (🎯T401). The message is durably queued for recovery; it is not a silent
// drop and not a bare "agent is not running".
const StatusReapedHeld = "reaped_held"

// LookupReapedRecord reports whether name carries a finished-and-reaped
// intent. A missing stamp means the name was never registered (or the stamp
// aged out) — that case stays an ordinary not-found.
func LookupReapedRecord(snap fleetintent.Snapshot, name string) (fleetintent.Record, bool) {
	name = strings.TrimSpace(name)
	if name == "" || snap.Agents == nil {
		return fleetintent.Record{}, false
	}
	rec, ok := snap.Agents[name]
	if !ok || fleetintent.Resolve(rec.State) != fleetintent.Reaped {
		return fleetintent.Record{}, false
	}
	return rec, true
}

// ReapedReportRef names the terminal report the reap rode on, when the
// 🎯T388 store still has it. Empty when unwired or never stored.
func (s *Server) ReapedReportRef(name string) string {
	dir := s.agentReportStateDir()
	if dir == "" {
		return ""
	}
	rec, err := agentreport.Latest(dir, name)
	if err != nil {
		return ""
	}
	return rec.ID
}

// FormatReapedSend is what the sender reads when the address is closed by
// auto-reap. It names the stamp, the report, and the exact recovery call —
// not a bare "not running".
func FormatReapedSend(name string, rec fleetintent.Record, reportID string, queued int, queueErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Agent %q was auto-deregistered (reaped-with-reason): %s.",
		name, rec.Describe())
	if reportID != "" {
		fmt.Fprintf(&b, " Terminal report id=%s (jevons_agent_report_read name=%q report_id=%q).",
			reportID, name, reportID)
	} else {
		fmt.Fprintf(&b, " Read the stored report with jevons_agent_report_read name=%q if one was saved.", name)
	}
	b.WriteString(" Recovery: jevons_agent_start name=")
	b.WriteString(name)
	b.WriteString(" … (a fresh start under a previously reaped name lifts the reaped intent when no registry row remains; or jevons_fleet_intent name=")
	b.WriteString(name)
	b.WriteString(" state=working then start).")
	switch {
	case queueErr != nil:
		fmt.Fprintf(&b, " MESSAGE NOT HELD — send queue refused the payload (%v); re-send after recovery.", queueErr)
	case queued > 0:
		fmt.Fprintf(&b, " MESSAGE HELD in the daemon send queue (%d pending); it drains when that name is started again.", queued)
	default:
		b.WriteString(" MESSAGE NOT HELD — queue depth is zero; re-send after recovery.")
	}
	return b.String()
}

// FormatReapedListSection lists finished-and-reaped names so agent_list
// distinguishes them from never-existed before the caller composes a send.
func FormatReapedListSection(snap fleetintent.Snapshot) string {
	var names []string
	for _, name := range snap.NotWorking() {
		if snap.AgentState(name) == fleetintent.Reaped {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Finished-and-reaped (recoverable; not live seats):\n")
	for _, name := range names {
		fmt.Fprintf(&b, "  %-20s %s\n", name, snap.Agents[name].Describe())
	}
	b.WriteString("Recovery: jevons_agent_start under the same name (or jevons_fleet_intent state=working then start). Gate feedback held in sendq drains on start.\n")
	return b.String()
}

// holdSendForReaped enqueues text for a reaped name and returns the
// reaped_held outcome. Call only when LookupReapedRecord succeeded.
func (s *Server) holdSendForReaped(name, text string, rec fleetintent.Record) agentSendResult {
	depth, qerr := s.enqueueAgentSend(name, text)
	reportID := s.ReapedReportRef(name)
	msg := FormatReapedSend(name, rec, reportID, depth, qerr)
	attrs := []any{
		"component", "agent_send",
		"name", name,
		"status", StatusReapedHeld,
		"queued", depth,
		"intent", string(rec.State),
		"by", rec.By,
		"reason", rec.Reason,
	}
	if qerr != nil {
		attrs = append(attrs, "queue_err", qerr.Error())
		slog.Error("agent_send reaped address — queue refused", attrs...)
	} else {
		slog.Info("agent_send reaped address — message held", attrs...)
	}
	return agentSendResult{
		Status:  StatusReapedHeld,
		Message: msg,
		Queued:  depth,
	}
}
