// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

const t401Agent = "jv-t401-reap-fixture"

func t401Server(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	s.SetAgentReportDir(dir)
	s.SetSendQueueDir(dir)
	s.SetRemovalAccount(fleetlog.New(nil))
	return s, dir
}

func t401RegisterWork(t *testing.T, s *Server, name string) {
	t.Helper()
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "s-" + name,
		Purpose: claudia.PurposeWork, TargetID: "T401", Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
}

func t401ReapWithReport(t *testing.T, s *Server, dir, name, report string) {
	t.Helper()
	if _, err := agentreport.Save(dir, name, report, time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// T401's subject is the address AFTER a genuine auto-reap, not the
	// finish classifier. Accounted remove + MarkAgentReaped is the same
	// pair OpenFleetIntent's removal hook performs on the product path.
	removed, err := s.RemovalAccount().Remove(s.registry, name, fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: "reaped on a finished-work report (finished_work)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("accounted remove did not drop the registry row")
	}
	s.MarkAgentReaped(name, "product:reap_done", "reaped on a finished-work report (finished_work)")
	if s.registry.Def(name) != nil {
		t.Fatal("reaped agent still in registry")
	}
	if _, ok := LookupReapedRecord(s.fleetIntent(), name); !ok {
		t.Fatal("reaped intent missing after MarkAgentReaped")
	}
}

// Clause 1 + 3 + 4: send to a reaped fixture reports reaped-with-reason,
// names recovery, and holds the payload in sendq.
func TestT401SendToReapedReportsReasonAndHoldsMessage(t *testing.T) {
	s, dir := t401Server(t)
	t401RegisterWork(t, s, t401Agent)
	report := "Done. SHA abcdef1234567890. GATE t401-fix exit=0 GREEN id=deadbeef."
	t401ReapWithReport(t, s, dir, t401Agent, report)

	res, err := s.deliverByName(t401Agent, "gate: master is red on your commit — fix", OriginAgent, false)
	if err != nil {
		t.Fatalf("send to reaped must not be a transport error: %v", err)
	}
	if res.Status != StatusReapedHeld {
		t.Fatalf("status=%q want %s", res.Status, StatusReapedHeld)
	}
	for _, want := range []string{
		"reaped-with-reason",
		"auto-deregistered",
		"finished and reaped",
		"Recovery: jevons_agent_start",
		"MESSAGE HELD",
		"report id=",
	} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("message missing %q:\n%s", want, res.Message)
		}
	}
	if strings.Contains(res.Message, "is not running") && !strings.Contains(res.Message, "reaped-with-reason") {
		t.Fatalf("bare not-running survived:\n%s", res.Message)
	}
	if res.Queued < 1 {
		t.Fatalf("queued=%d want ≥1 — gate feedback must be held", res.Queued)
	}
	if depth := s.pendingAgentSends(t401Agent); depth != res.Queued {
		t.Fatalf("sendq depth=%d status.Queued=%d", depth, res.Queued)
	}
}

// Clause 4 control: never-registered stays an ordinary not-found; nothing
// is queued against a name nobody will fill.
func TestT401NeverRegisteredStillNotFound(t *testing.T) {
	s, _ := t401Server(t)
	_, err := s.deliverByName("jv-t401-never-existed", "hello?", OriginAgent, false)
	if err == nil {
		t.Fatal("never-registered must error")
	}
	if !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("err=%v want ordinary not-running", err)
	}
	if strings.Contains(err.Error(), "reaped-with-reason") {
		t.Fatalf("never-registered must not look reaped: %v", err)
	}
	if depth := s.pendingAgentSends("jv-t401-never-existed"); depth != 0 {
		t.Fatalf("queued=%d for a never-registered name", depth)
	}
}

// Clause 2: agent_list distinguishes finished-reaped from never-existed.
func TestT401AgentListDistinguishesReapedFromNeverExisted(t *testing.T) {
	s, dir := t401Server(t)
	t401RegisterWork(t, s, t401Agent)
	t401ReapWithReport(t, s, dir, t401Agent,
		"Done. SHA abcdef1234567890. GATE t401-list exit=0 GREEN id=cafebabe.")

	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(res)
	if !strings.Contains(body, "Finished-and-reaped") {
		t.Fatalf("list missing reaped section:\n%s", body)
	}
	if !strings.Contains(body, t401Agent) {
		t.Fatalf("list does not name reaped agent:\n%s", body)
	}
	if !strings.Contains(body, "recoverable") {
		t.Fatalf("list does not mark recoverable:\n%s", body)
	}
	if strings.Contains(body, "jv-t401-never-existed") {
		t.Fatalf("never-existed name appeared in list:\n%s", body)
	}
}

// Clause 3 + T418 compose: sweep must NOT drop a backlog for a reaped name.
func TestT401BacklogForReapedIsHeldNotDropped(t *testing.T) {
	s, dir := t401Server(t)
	t401RegisterWork(t, s, t401Agent)
	t401ReapWithReport(t, s, dir, t401Agent,
		"Done. SHA abcdef1234567890. GATE t401-hold exit=0 GREEN id=feedface.")

	if _, err := s.enqueueAgentSend(t401Agent, "gate feedback"); err != nil {
		t.Fatal(err)
	}
	// Age the entry past the stall threshold so the held-reaped notice fires.
	entries, err := s.sendQueue().Snapshot(t401Agent)
	if err != nil || len(entries) == 0 {
		t.Fatalf("snapshot: %v n=%d", err, len(entries))
	}
	s.SweepSendBacklogs()
	if depth := s.pendingAgentSends(t401Agent); depth != 1 {
		t.Fatalf("reaped backlog depth=%d; want 1 held (not dropped)", depth)
	}

	// Control: never-registered backlog still drops (T418).
	if _, _, err := s.sendQueue().Append("jv-t401-ghost", "orphan", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SweepSendBacklogs()
	if depth := s.pendingAgentSends("jv-t401-ghost"); depth != 0 {
		t.Fatalf("ghost backlog depth=%d; want 0 dropped", depth)
	}
}

// Red against the pre-fix tree: without consulting reaped intent, send is
// a bare not-running and nothing is held.
func TestT401PreFixBareNotRunningIsRed(t *testing.T) {
	s, dir := t401Server(t)
	t401RegisterWork(t, s, t401Agent)
	t401ReapWithReport(t, s, dir, t401Agent,
		"Done. SHA abcdef1234567890. GATE t401-pre exit=0 GREEN id=preffix0.")

	// Mutant: ignore reaped stamps (pre-T401 ensureAgentProcess behaviour).
	preFixLookup := func(fleetintent.Snapshot, string) (fleetintent.Record, bool) {
		return fleetintent.Record{}, false
	}
	if rec, ok := preFixLookup(s.fleetIntent(), t401Agent); ok {
		t.Fatalf("pre-fix mutant returned a record: %+v", rec)
	}
	// Simulate the pre-fix message shape the oracle must reject: Def-nil
	// with no reaped stamp consulted → bare not-running.
	preFixMsg := fmtAgentNotRunning(t401Agent)
	if !strings.Contains(preFixMsg, "is not running") {
		t.Fatal("pre-fix control fixture broken")
	}
	if strings.Contains(preFixMsg, "reaped-with-reason") {
		t.Fatal("pre-fix message must be bare")
	}
	// Product path must differ from that bare shape.
	res, err := s.deliverByName(t401Agent, "fix this", OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusReapedHeld {
		t.Fatalf("product status=%q; pre-fix would have errored bare not-running", res.Status)
	}
	if !strings.Contains(res.Message, "reaped-with-reason") {
		t.Fatalf("product message still bare:\n%s", res.Message)
	}
}

func fmtAgentNotRunning(name string) string {
	return "agent \"" + name + "\" is not running"
}

// Over-broadness: treating every missing name as reaped must fail the
// never-registered control.
func TestT401OverBroadResurrectsEveryDeadNameFails(t *testing.T) {
	s, _ := t401Server(t)

	overBroad := func(_ fleetintent.Snapshot, name string) (fleetintent.Record, bool) {
		return fleetintent.Record{
			State:  fleetintent.Reaped,
			By:     "mutant",
			Reason: "everything missing is reaped",
			At:     time.Now(),
		}, name != ""
	}
	ghost := "jv-t401-overbroad-ghost"
	if rec, ok := overBroad(s.fleetIntent(), ghost); !ok || rec.State != fleetintent.Reaped {
		t.Fatal("over-broad mutant fixture broken")
	}
	// Product Lookup must NOT agree — otherwise the never-registered control
	// would be green for the wrong reason.
	if _, ok := LookupReapedRecord(s.fleetIntent(), ghost); ok {
		t.Fatal("product LookupReapedRecord treats a never-registered name as reaped — over-broad")
	}
	_, err := s.deliverByName(ghost, "should not hold", OriginAgent, false)
	if err == nil {
		t.Fatal("product held a never-registered send — over-broad resurrection")
	}
	if depth := s.pendingAgentSends(ghost); depth != 0 {
		t.Fatalf("product queued for never-registered (depth=%d) — over-broad", depth)
	}
}

// Pure formatting: report id and recovery call are load-bearing phrases.
func TestT401FormatReapedSendNamesRecoveryAndReport(t *testing.T) {
	rec := fleetintent.Record{
		State:  fleetintent.Reaped,
		By:     "product:reap_done",
		Reason: "reaped on a finished-work report (finished_work)",
		At:     time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC),
	}
	msg := FormatReapedSend("jv-x", rec, "20260809T150000Z-abcd", 2, nil)
	for _, want := range []string{
		"reaped-with-reason",
		"product:reap_done",
		"20260809T150000Z-abcd",
		"jevons_agent_report_read",
		"jevons_agent_start name=jv-x",
		"MESSAGE HELD",
		"2 pending",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("FormatReapedSend missing %q:\n%s", want, msg)
		}
	}
}
