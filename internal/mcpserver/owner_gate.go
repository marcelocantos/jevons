// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/ownergate"
)

// jevons_owner_gate is the writer half of 🎯T449 — the tool the accepting PO
// calls at the moment it accepts a gated report whose only residue is the
// owner's taste verdict.
//
// WHY A WRITER AND NOT AN INFERENCE. Acceptance clause 4 is the load-bearing
// one: the state is written where it is earned, by the PO that read the
// oracle evidence, not by a later sweep reading the prose and guessing. An
// inferred state is the same guess this target exists to eliminate — the
// sweep already inferred "nothing has been done here" from `status:
// identified` and spawned an implementer against finished work twice.
//
// WHAT IT REFUSES. Recording the state removes a target from unattended
// consumption, which is precisely what an agent under completion pressure
// would like to do to work it has not finished. So the tool refuses to write
// the claim without evidence that names something a reader can go and check:
// a commit SHA, a 🎯T386 GATE id, or a named green oracle. The refusal is the
// product, not a nicety.
func (s *Server) registerOwnerGateTool() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_owner_gate",
			mcp.WithDescription("Record — or answer — the 🎯T449 awaiting-owner-verdict state on a bullseye target: the code has LANDED and gated, and the sole remaining acceptance residue is a class-3 owner taste verdict (hard-reload accept/reject). op=record assigns the target to the owner with a structured, evidence-bearing reason, so frontier-consume parks instead of spawning an implementer against finished work and every other reader sees \"built, your turn\" rather than \"untouched\". Call it as the accepting PO, at the moment you accept the gated report — never later, and never for work that has not landed. op=answer records the owner's accept/reject and clears the assignment so the leaf returns to normal handling (accept → go achieve it; reject → resume from the landed commit, do not restart from scratch). Requires the bullseye CLI on PATH."),
			mcp.WithString("cwd", mcp.Required(), mcp.Description("Repo directory containing bullseye.yaml (or a parent to discover from)")),
			mcp.WithString("target", mcp.Required(), mcp.Description("Target id, e.g. T383 or 🎯T383")),
			mcp.WithString("op", mcp.Description("record (default) — claim awaiting-owner-verdict; answer — record the owner's verdict and unassign")),
			mcp.WithString("question", mcp.Description("op=record: the single thing the owner must judge, phrased so the answer is accept or reject")),
			mcp.WithString("evidence", mcp.Description("op=record: what landed — commit SHA, GATE id (🎯T386), named green oracles. Refused without it")),
			mcp.WithString("by", mcp.Description("Agent recording the claim (defaults to the calling agent's identity)")),
			mcp.WithString("verdict", mcp.Description("op=answer: accept or reject")),
			mcp.WithString("note", mcp.Description("op=answer: optional note from the owner")),
		),
		s.handleOwnerGate,
	)
}

func (s *Server) handleOwnerGate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	cwd, err := resolveOwnerGateCwd(str(args["cwd"]))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	target := normalizeTargetID(str(args["target"]))
	if target == "" {
		return mcp.NewToolResultError("target is required (e.g. T383)"), nil
	}
	// `by` is who is making the claim. Left blank it renders as "unknown",
	// which is honest: the MCP transport does not carry a caller identity,
	// and inventing one would be a fabricated attestation (🎯T31).
	by := strings.TrimSpace(str(args["by"]))

	switch strings.ToLower(strings.TrimSpace(str(args["op"]))) {
	case "", "record":
		rec := ownergate.Record{
			Question:   str(args["question"]),
			Evidence:   str(args["evidence"]),
			RecordedBy: by,
			Now:        time.Now(),
		}
		reason, err := rec.Reason()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("owner gate refused (🎯T449): %v", err)), nil
		}
		out, err := runBullseye("commit", "--op", "assign", "--cwd", cwd,
			"--id", target, "--owner", ownergate.OwnerHandle, "--reason", reason)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bullseye assign failed: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"🎯%s recorded as %s (🎯T449). Frontier-consume will park it, not spawn against it; the owner's accept/reject is the only thing left.\nReason written:\n%s\n\n%s",
			target, ownergate.MarkerAwaiting, reason, out)), nil

	case "answer":
		verdict, err := ownergate.ParseVerdict(str(args["verdict"]))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line := ownergate.FormatAnswer(verdict, str(args["note"]), by, time.Now())
		// Unassign first: the gate is answered, so the target must return to
		// normal handling even if the follow-up narration fails. Leaving it
		// parked on an answered gate is the same defect one step later.
		out, err := runBullseye("commit", "--op", "unassign", "--cwd", cwd, "--id", target)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bullseye unassign failed: %v\n%s", err, out)), nil
		}
		next := "Owner accepted: achieve the target now, citing the landed commit and the gate ids."
		if verdict == ownergate.VerdictReject {
			next = "Owner rejected: resume from the landed commit — do not reopen from scratch."
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"🎯%s gate answered — %s\n%s\n\n%s", target, line, next, out)), nil

	default:
		return mcp.NewToolResultError("op must be record or answer"), nil
	}
}

// resolveOwnerGateCwd expands ~ and checks the directory exists, the same
// contract jevons_target_file takes.
func resolveOwnerGateCwd(raw string) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if strings.HasPrefix(cwd, "~/") {
		if home, herr := os.UserHomeDir(); herr == nil {
			cwd = filepath.Join(home, cwd[2:])
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("cwd: %v", err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("cwd is not a directory: %s", abs)
	}
	return abs, nil
}

// normalizeTargetID strips the 🎯 prefix and validates the shape, so a
// mistyped id fails here rather than as an opaque bullseye error.
func normalizeTargetID(raw string) string {
	id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "🎯"))
	if !isTargetID(id) {
		return ""
	}
	return id
}
