// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/planstep"
)

// 🎯T254.3 — targets can carry ordered plan steps that agents walk and
// resume across restarts. This wires the hermetic internal/planstep
// package (claim/resume/complete) up as MCP tools so a live implementer
// can actually use it against a real mission, rooted at
// <StateDir>/planstep so state survives a daemon restart.

var (
	planStepStoreMu sync.Mutex
	planStepStore   *planstep.Store
)

func (s *Server) planStepStoreOrError() (*planstep.Store, *mcp.CallToolResult) {
	planStepStoreMu.Lock()
	defer planStepStoreMu.Unlock()
	if planStepStore != nil {
		return planStepStore, nil
	}
	dir := ""
	if s != nil {
		dir = strings.TrimSpace(s.stateDir)
	}
	if dir == "" {
		return nil, mcp.NewToolResultError("planstep: no state dir configured")
	}
	st, err := planstep.NewStore(planstep.DefaultDir(dir))
	if err != nil {
		return nil, mcp.NewToolResultError(fmt.Sprintf("planstep: %v", err))
	}
	planStepStore = st
	return st, nil
}

// registerPlanStepTools exposes jevons_plan_step: set | claim | complete |
// show, over one durable ordered-step store per target (🎯T254.3).
func (s *Server) registerPlanStepTools() {
	s.addTool(
		mcp.NewTool("jevons_plan_step",
			mcp.WithDescription("Ordered plan steps for a bullseye target that agents walk and resume across restarts (🎯T254.3). op=set defines ordered steps (name/acceptance pairs, semicolon-separated) once per target — refuses to overwrite unless overwrite=true. op=claim returns the step to work on right now: the first pending step, or — if a step is already in_progress (including after a restart, since state is durable) — that SAME step again, so a resumed agent picks up without a fresh brief. op=complete marks a step done (refuses out-of-order/duplicate completion) and unblocks the next step. op=show reads the plan without mutating it."),
			mcp.WithString("op", mcp.Required(), mcp.Description("set | claim | complete | show")),
			mcp.WithString("target", mcp.Required(), mcp.Description("Bullseye target id the plan belongs to, e.g. T254.3")),
			mcp.WithString("steps", mcp.Description("op=set: ordered steps as \"name|acceptance\" pairs, one per line or semicolon-separated, e.g. \"design schema|schema exists; hermetic test|test green\"")),
			mcp.WithBoolean("overwrite", mcp.Description("op=set: allow replacing an existing plan (default false)")),
			mcp.WithString("claimant", mcp.Description("op=claim: your agent name")),
			mcp.WithString("step_id", mcp.Description("op=complete: the step id to mark done (from a prior claim/show)")),
		),
		s.handlePlanStep,
	)
}

func (s *Server) handlePlanStep(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	op := strings.TrimSpace(str(args["op"]))
	target := strings.TrimSpace(str(args["target"]))
	if target == "" {
		return mcp.NewToolResultError("target is required"), nil
	}
	st, errRes := s.planStepStoreOrError()
	if errRes != nil {
		return errRes, nil
	}

	switch op {
	case "set":
		raw := str(args["steps"])
		steps, parseErr := parsePlanSteps(raw)
		if parseErr != nil {
			return mcp.NewToolResultError(parseErr.Error()), nil
		}
		overwrite, _ := args["overwrite"].(bool)
		plan, err := st.SetPlan(target, steps, overwrite)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("planstep set: %v", err)), nil
		}
		return mcp.NewToolResultText(formatPlanStepPlan(plan)), nil

	case "claim":
		claimant := strings.TrimSpace(str(args["claimant"]))
		if claimant == "" {
			claimant = "unknown"
		}
		step, err := st.ClaimNext(target, claimant)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("planstep claim: %v", err)), nil
		}
		if step == nil {
			return mcp.NewToolResultText(fmt.Sprintf("plan for %s is fully walked — nothing left to claim", target)), nil
		}
		return mcp.NewToolResultText(formatPlanStep(*step)), nil

	case "complete":
		stepID := strings.TrimSpace(str(args["step_id"]))
		if stepID == "" {
			return mcp.NewToolResultError("step_id is required for op=complete"), nil
		}
		plan, err := st.CompleteStep(target, stepID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("planstep complete: %v", err)), nil
		}
		return mcp.NewToolResultText(formatPlanStepPlan(plan)), nil

	case "show":
		plan, err := st.Load(target)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("planstep show: %v", err)), nil
		}
		return mcp.NewToolResultText(formatPlanStepPlan(plan)), nil

	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown op %q: want set|claim|complete|show", op)), nil
	}
}

// parsePlanSteps parses "name|acceptance; name|acceptance; ..." (newlines
// also accepted as separators) into ordered planstep.Step values.
func parsePlanSteps(raw string) ([]planstep.Step, error) {
	raw = strings.ReplaceAll(raw, "\n", ";")
	parts := strings.Split(raw, ";")
	var steps []planstep.Step
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		acceptance := ""
		if i := strings.Index(part, "|"); i >= 0 {
			name = strings.TrimSpace(part[:i])
			acceptance = strings.TrimSpace(part[i+1:])
		}
		if name == "" {
			continue
		}
		steps = append(steps, planstep.Step{Name: name, Acceptance: acceptance})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("steps: no steps parsed from %q (want \"name|acceptance; name|acceptance\")", raw)
	}
	return steps, nil
}

func formatPlanStep(st planstep.Step) string {
	return fmt.Sprintf("id=%s name=%q status=%s claimed_by=%q acceptance=%q",
		st.ID, st.Name, st.Status, st.ClaimedBy, st.Acceptance)
}

func formatPlanStepPlan(p *planstep.Plan) string {
	if p == nil || len(p.Steps) == 0 {
		return "(empty plan)"
	}
	done, total, complete := p.Progress()
	var b strings.Builder
	fmt.Fprintf(&b, "target=%s progress=%d/%d complete=%v\n", p.TargetID, done, total, complete)
	for i, st := range p.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, formatPlanStep(st))
	}
	return b.String()
}
