// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1083: a read-only worker's finish-report with `status done` was read
// as unknown, so the 🎯T165 reap never fired and the parent reaped by hand.
// These drive the report through the real agentEventSink terminal path
// (🎯T419), not only the pure classifier.

func t1083ReadOnlyReport(status string) string {
	var b strings.Builder
	b.WriteString("```jevons\n")
	b.WriteString("jevons: kind finish-report\n")
	b.WriteString("jevons: target T1083\n")
	if status != "" {
		b.WriteString("jevons: status " + status + "\n")
	}
	b.WriteString("jevons: sha 31134215\n")
	b.WriteString("jevons: silent-ledger none\n")
	b.WriteString("```\n\nCounted lines in internal/envelope at HEAD; nothing committed.")
	return b.String()
}

type t1083Journal struct {
	mu   sync.Mutex
	rows []map[string]any
}

func (j *t1083Journal) log(component, decision string, fields map[string]any) {
	if component != compAgentLifecycle {
		return
	}
	row := map[string]any{"decision": decision}
	for k, v := range fields {
		row[k] = v
	}
	j.mu.Lock()
	j.rows = append(j.rows, row)
	j.mu.Unlock()
}

func (j *t1083Journal) find(decision, outcome string) map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, r := range j.rows {
		if r["decision"] == decision && r["outcome"] == outcome {
			return r
		}
	}
	return nil
}

func TestT1083StatusDoneKeepsSeatAndTellsAuthorTheSlot(t *testing.T) {
	const agent = "jv-t1083-status-done"
	s, reg := reapSinkServer(t, agent)
	j := &t1083Journal{}
	s.eventLogger = j.log

	report := t1083ReadOnlyReport("done")
	if got := envelope.ClassifyDone(report); got != envelope.DoneUnknown {
		t.Fatalf("ClassifyDone=%s, want unknown", got)
	}
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})

	if reg.Def(agent) == nil {
		t.Fatal("status done reaped the seat; the author had no chance to resend")
	}
	row := j.find("envelope", "correction_sent")
	if row == nil {
		t.Fatalf("no correction_sent journal line; rows=%v", j.rows)
	}
	if row["slot"] != "status" || row["kind"] != string(envelope.KindFinishReport) {
		t.Fatalf("correction_sent=%v, want slot status on a finish-report", row)
	}
	if j.find("reap_done", "ok") != nil {
		t.Fatal("reap_done ok logged for an unknown report")
	}
	notice := envelope.CorrectionNotice(report)
	for _, want := range []string{"`jevons: status done`", "in-progress | live | landed | shipped | blocked", envelope.DoneShape} {
		if !strings.Contains(notice, want) {
			t.Errorf("author notice missing %q:\n%s", want, notice)
		}
	}
}

func TestT1083StatusOmittedReapsThroughSink(t *testing.T) {
	const agent = "jv-t1083-status-omitted"
	s, reg := reapSinkServer(t, agent)
	j := &t1083Journal{}
	s.eventLogger = j.log

	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: t1083ReadOnlyReport(""), StopReason: "end_turn"})

	if reg.Def(agent) != nil {
		t.Fatalf("done-shape report left the seat registered; rows=%v", j.rows)
	}
	if j.find("envelope", "correction_sent") != nil {
		t.Fatal("done-shape report drew a correction notice")
	}
	row := j.find("reap_done", "ok")
	if row == nil || row["reason"] != "finished_work" {
		t.Fatalf("reap_done ok row=%v, want reason finished_work; rows=%v", row, j.rows)
	}
}

// TestT1083PublishedDoneShapeReapsThroughSink: the shape the notice and the
// instructions show is itself reaped on the real path.
func TestT1083PublishedDoneShapeReapsThroughSink(t *testing.T) {
	const agent = "jv-t1083-published-shape"
	s, reg := reapSinkServer(t, agent)
	s.eventLogger = func(string, string, map[string]any) {}
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: envelope.DoneShape + "\n\nSurveyed; nothing committed.", StopReason: "end_turn"})
	if reg.Def(agent) != nil {
		t.Fatal("envelope.DoneShape did not reap")
	}
}
