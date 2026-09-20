// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T731 — a report that reaches a parent after its seat was reaped says so,
// and is not delivered to that parent more than once.
//
// notify() stores the report, delivers to the parent, then maybeReapDoneWorkAgent
// removes the row. If the parent is mid-turn the copy sits in sendq and is
// flushed by drainAgentSendQueueOnce AFTER the author is gone, with nothing on
// the artifact to say the seat no longer exists. T428/T568 refuse byte-identical
// batches only on the overseer arm, so the parent can receive the same report
// id again looking like a live claim.
//
// Product: at actual send (deliverByName to a fleet parent, and drain of a
// queued copy), a report whose author is reaped is prefixed with the reap
// time. The same report id is offered to the same parent at most once.
// Live seats stay unmarked. The first copy is still delivered (🎯T690).

const (
	// StatusSuppressedDuplicateReport is a successful refusal: this parent
	// already has this report id. Distinct from StatusSuppressedReplay
	// (byte-identical overseer batches) because a reap banner changes bytes.
	StatusSuppressedDuplicateReport = "suppressed_duplicate_report"

	agentRespondedPrefix    = "[Agent "
	reapedSeatPrefix        = "[reaped seat "
	parentReportOfferedFile = "parent-report-offered.jsonl"
)

// FormatReapedReportBanner is the daemon prefix that makes a post-reap
// delivery read as historical. Outside the worker envelope so SHA/gate text
// is untouched. StripPrefixes peels it so 🎯T509 still sees the fence at line 1.
func FormatReapedReportBanner(agent string, rec fleetintent.Record) string {
	agent = strings.TrimSpace(agent)
	at := "unknown-time"
	if !rec.At.IsZero() {
		at = rec.At.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("[reaped seat %s at %s — this report describes that moment, not current ledger or fleet state (🎯T731)]",
		agent, at)
}

func formatAgentResponded(agent string, h agentreport.Handle, body string) string {
	agent = strings.TrimSpace(agent)
	if !h.Empty() && strings.TrimSpace(h.Agent) == agent {
		return fmt.Sprintf("[Agent %s responded] report_id=%s\n%s", agent, h.ReportID, body)
	}
	return fmt.Sprintf("[Agent %s responded]\n%s", agent, body)
}

func withAgentReportID(msg string, h agentreport.Handle) string {
	if h.Empty() {
		return msg
	}
	old := fmt.Sprintf("[Agent %s responded]", h.Agent)
	neu := fmt.Sprintf("[Agent %s responded] report_id=%s", h.Agent, h.ReportID)
	if strings.Contains(msg, neu) {
		return msg
	}
	return strings.Replace(msg, old, neu, 1)
}

func parseAgentRespondedLine(line string) (name, reportID string, ok bool) {
	line = strings.TrimRight(line, "\r")
	if !strings.HasPrefix(line, agentRespondedPrefix) {
		return "", "", false
	}
	const mark = " responded]"
	i := strings.Index(line, mark)
	if i < 0 {
		return "", "", false
	}
	name = strings.TrimSpace(line[len(agentRespondedPrefix):i])
	if name == "" {
		return "", "", false
	}
	rest := strings.TrimSpace(line[i+len(mark):])
	if rest == "" {
		return name, "", true
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "|"))
	if !strings.HasPrefix(rest, "report_id=") {
		return name, "", true
	}
	id := strings.TrimSpace(strings.TrimPrefix(rest, "report_id="))
	if id == "" || strings.ContainsAny(id, " \t]") {
		return name, "", true
	}
	return name, id, true
}

func findAgentResponded(text string) (name, reportID string, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		if n, id, found := parseAgentRespondedLine(line); found {
			return n, id, true
		}
	}
	return "", "", false
}

type parentReportPrep struct {
	Text     string
	ReportID string
	Agent    string
	Suppress bool
	Reason   string
}

func hasReapedSeatBanner(text string) bool {
	s := strings.TrimLeft(text, " \t\r\n")
	return strings.HasPrefix(s, reapedSeatPrefix)
}

func (s *Server) prepareParentReport(dest, text string, fulfilling bool) parentReportPrep {
	out := parentReportPrep{Text: text}
	agent, reportID, ok := findAgentResponded(text)
	if !ok {
		return out
	}
	out.Agent = agent
	out.ReportID = reportID
	if !fulfilling && reportID != "" && s.parentReportAlreadyOffered(dest, reportID) {
		out.Suppress = true
		out.Reason = fmt.Sprintf(
			"Not delivered to parent %q — report id %s was already offered to this parent (🎯T731). Nothing was lost.",
			dest, reportID)
		return out
	}
	if rec, reaped := LookupReapedRecord(s.fleetIntent(), agent); reaped && !hasReapedSeatBanner(text) {
		out.Text = FormatReapedReportBanner(agent, rec) + "\n" + text
	}
	return out
}

func parentReportOfferStatus(status string) bool {
	switch status {
	case "sent", "queued", "steered",
		"interrupted_sent", "interrupted_queued", "rehydrated_sent":
		return true
	default:
		return false
	}
}

func parentReportKey(parent, reportID string) string {
	return strings.TrimSpace(parent) + "\x00" + strings.TrimSpace(reportID)
}

func (s *Server) parentReportAlreadyOffered(parent, reportID string) bool {
	parent = strings.TrimSpace(parent)
	reportID = strings.TrimSpace(reportID)
	if s == nil || parent == "" || reportID == "" {
		return false
	}
	s.loadParentReportOffered()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.parentReportOffered[parentReportKey(parent, reportID)]
	return ok
}

func (s *Server) noteParentReportOffered(parent, reportID string) {
	parent = strings.TrimSpace(parent)
	reportID = strings.TrimSpace(reportID)
	if s == nil || parent == "" || reportID == "" {
		return
	}
	s.loadParentReportOffered()
	key := parentReportKey(parent, reportID)
	s.mu.Lock()
	if s.parentReportOffered == nil {
		s.parentReportOffered = map[string]struct{}{}
	}
	_, dup := s.parentReportOffered[key]
	s.parentReportOffered[key] = struct{}{}
	dir := strings.TrimSpace(s.stateDir)
	s.mu.Unlock()
	if dup || dir == "" {
		return
	}
	rec := struct {
		Parent   string `json:"parent"`
		ReportID string `json:"report_id"`
		At       string `json:"at"`
	}{Parent: parent, ReportID: reportID, At: time.Now().UTC().Format(time.RFC3339)}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	path := filepath.Join(dir, parentReportOfferedFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
}

func (s *Server) loadParentReportOffered() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.parentReportOfferedLoaded {
		s.mu.Unlock()
		return
	}
	s.parentReportOfferedLoaded = true
	if s.parentReportOffered == nil {
		s.parentReportOffered = map[string]struct{}{}
	}
	dir := strings.TrimSpace(s.stateDir)
	s.mu.Unlock()
	if dir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, parentReportOfferedFile))
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			Parent   string `json:"parent"`
			ReportID string `json:"report_id"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Parent == "" || rec.ReportID == "" {
			continue
		}
		s.parentReportOffered[parentReportKey(rec.Parent, rec.ReportID)] = struct{}{}
	}
}
