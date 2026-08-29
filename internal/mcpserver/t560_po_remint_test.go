package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

// 🎯T560: killing the PO does not take its in-progress work agents.

func TestT560PlanKillPreservesDescendantsByDefault(t *testing.T) {
	plan := PlanKill("po", []string{"w1", "w2", "po", ""}, false)
	if len(plan.Removed) != 0 {
		t.Fatalf("default kill removed descendants: %v", plan.Removed)
	}
	if got := strings.Join(plan.Preserved, ","); got != "w1,w2" {
		t.Fatalf("preserved = %q", got)
	}
	plan = PlanKill("po", []string{"w1", "w2"}, true)
	if got := strings.Join(plan.Removed, ","); got != "w1,w2" || len(plan.Preserved) != 0 {
		t.Fatalf("subtree=true plan = %+v", plan)
	}
}

func TestT560KillPOPreservesWorkerAcrossRemint(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "po", "actor": "jevons"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("kill: %s", toolText(res))
	}
	if reg.Def("po") != nil {
		t.Fatal("po still registered")
	}
	w := reg.Def("worker")
	if w == nil {
		t.Fatal("worker was killed with the PO (T560 regression)")
	}
	if w.Parent != "po" {
		t.Fatalf("worker parent = %q, want po", w.Parent)
	}
	if !strings.Contains(toolText(res), "Preserved 1 descendant") {
		t.Fatalf("result does not name the preserved seat: %s", toolText(res))
	}
	// Cold remint: the same name re-registers and lineage is whole again.
	if err := reg.Register(claudia.AgentDef{
		Name: "po", WorkDir: w.WorkDir, SessionID: "s-po-2", Provider: "grok", Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	if got := reg.Descendants("po"); len(got) != 1 || got[0] != "worker" {
		t.Fatalf("descendants after remint = %v, want [worker]", got)
	}
	if !reg.IsAncestor("jevons", "worker") {
		t.Fatal("overseer lineage to worker not restored after remint")
	}
}

func TestT560ExplicitSubtreeKillStillRemovesDescendants(t *testing.T) {
	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "po", "actor": "jevons", "subtree": true}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("kill: %s", toolText(res))
	}
	if reg.Def("po") != nil || reg.Def("worker") != nil {
		t.Fatal("subtree=true left rows behind")
	}
	if !strings.Contains(toolText(res), "Also killed 1 descendant") {
		t.Fatalf("result: %s", toolText(res))
	}
}

// A preserved child with a held sendq is not restarted or reparented: it
// keeps its row and its queue, and the PO's own sendq does not refuse the kill.
func TestT560PreservedHeldChildKeepsRowAndQueue(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	const child = "jv-t530-drain"
	if _, err := s.enqueueAgentSend("jevons-po", "stalled PO queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.enqueueAgentSend(child, "gate feedback still draining"); err != nil {
		t.Fatal(err)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jevons-po", "actor": "jevons"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("PO remint refused: %s", toolText(res))
	}
	def := s.registry.Def(child)
	if def == nil || def.Parent != "jevons-po" {
		t.Fatalf("child def=%+v want preserved under jevons-po", def)
	}
	if strings.Contains(toolText(res), "restarted") {
		t.Fatalf("preserved seat must not be drain-restarted: %s", toolText(res))
	}
	if s.pendingAgentSends(child) != 1 || s.pendingAgentSends("jevons-po") != 1 {
		t.Fatal("sendq depths must survive the preserve kill")
	}
}
