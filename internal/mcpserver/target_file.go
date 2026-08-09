// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/rsi"
)

// registerTargetFileTool exposes light-path bullseye filing for 🎯T93/T95
// (target: asides). Runs `bullseye commit --op track` in a repo cwd.
// 🎯T226: always allocates a new id (near-dup file attach removed; no silent attach).
func (s *Server) registerTargetFileTool() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_target_file",
			mcp.WithDescription("File a bullseye target in a repo ledger (light path for owner target: asides — 🎯T93/T95). Requires bullseye CLI on PATH. Always allocates a new 🎯 id (🎯T226: no near-duplicate attach). After success, confirm the id to the owner; the UI auto-closes the filing aside when it sees the confirmation marker. For filings that answer an RSI coach judgment, pass fingerprint= (from the judgment wire text) + evidence=: the 🎯T333 quality bar then applies (no bare phrase-friction leaves; concrete acceptance + evidence pointer required) and the disposition is recorded automatically."),
			mcp.WithString("cwd", mcp.Required(), mcp.Description("Repo directory containing bullseye.yaml (or parent to discover)")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Desired-state assertion (target name)")),
			mcp.WithString("acceptance", mcp.Description("Acceptance criterion (single string; more can be space-separated or repeated via context)")),
			mcp.WithString("context", mcp.Description("Optional context / why.")),
			mcp.WithString("fingerprint", mcp.Description("RSI coach judgment fingerprint this filing answers (🎯T333). Enables the quality bar and auto-records disposition=file.")),
			mcp.WithString("evidence", mcp.Description("Evidence pointer (session/event id). Required when fingerprint is set.")),
		),
		s.handleTargetFile,
	)
}

func (s *Server) handleTargetFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	cwd := strings.TrimSpace(str(args["cwd"]))
	name := strings.TrimSpace(str(args["name"]))
	acceptance := strings.TrimSpace(str(args["acceptance"]))
	contextText := strings.TrimSpace(str(args["context"]))
	fingerprint := strings.TrimSpace(str(args["fingerprint"]))
	evidence := strings.TrimSpace(str(args["evidence"]))
	if cwd == "" || name == "" {
		return mcp.NewToolResultError("cwd and name are required"), nil
	}

	// 🎯T333 quality bar: coach-linked filings must carry a mechanism, concrete
	// acceptance, and an evidence pointer — bare phrase-friction leaves are refused.
	qualityNote := ""
	if fingerprint != "" {
		q := rsi.ClassifyFilingQuality(name, []string{acceptance}, evidence)
		if !q.OK {
			return mcp.NewToolResultError(fmt.Sprintf(
				"filing refused by 🎯T333 quality bar:\n  - %s\nRewrite as a desired-state assertion with a mechanism, add concrete acceptance, and cite evidence (session/event id).",
				strings.Join(q.Reasons, "\n  - "))), nil
		}
	} else if rsi.IsBarePhraseFrictionName(name) {
		qualityNote = "\n⚠ 🎯T333 quality note: this name reads as a bare phrase-friction leaf. Prefer a desired-state assertion with a mechanism, concrete acceptance, and an evidence pointer."
	}

	if acceptance == "" {
		acceptance = rsi.DefaultFilingAcceptance
	}
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[2:])
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cwd: %v", err)), nil
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return mcp.NewToolResultError(fmt.Sprintf("cwd is not a directory: %s", abs)), nil
	}

	// 🎯T226: always track — never attach to an existing open leaf by name/acceptance.
	cmdArgs := []string{
		"commit", "--op", "track",
		"--cwd", abs,
		"--name", name,
		"--acceptance", acceptance,
	}
	if contextText != "" {
		cmdArgs = append(cmdArgs, "--context", contextText)
	}
	out, err := runBullseye(cmdArgs...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("bullseye commit failed: %v\n%s", err, out)), nil
	}
	id := parseBullseyeTrackID(out)
	if id == "" {
		// Still success if bullseye printed something useful.
		return mcp.NewToolResultText(fmt.Sprintf(
			"Target filed (parse id from output).%s\n__TARGET_FILED__:unknown\n\n%s", qualityNote, out)), nil
	}

	// 🎯T333: close the judgment loop — record disposition=file automatically.
	dispositionNote := ""
	if fingerprint != "" {
		if coach := s.RSICoach(); coach != nil && coach.Dispositions() != nil {
			_, derr := coach.Dispositions().SetDisposition(rsi.SetDispositionArgs{
				Fingerprint: fingerprint,
				Disposition: rsi.DispositionFile,
				TargetID:    id,
				TargetCwd:   abs,
				Evidence:    evidence,
				Now:         time.Now().UTC(),
			})
			if derr != nil {
				dispositionNote = fmt.Sprintf("\n⚠ disposition record failed (🎯T333): %v", derr)
			} else {
				dispositionNote = fmt.Sprintf("\nDisposition recorded (🎯T333): fp=%s → file 🎯%s", fingerprint, id)
			}
		} else {
			dispositionNote = "\n⚠ disposition not recorded: rsi coach store unavailable"
		}
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Filed 🎯%s — %s%s%s\n__TARGET_FILED__:%s\n\n%s", id, name, dispositionNote, qualityNote, id, out)), nil
}

// boolArg coerces MCP JSON bool / string into bool.
func boolArg(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// runBullseye shells out to bullseye on PATH. Tests can override.
var runBullseye = func(args ...string) (string, error) {
	cmd := exec.Command("bullseye", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// parseBullseyeTrackID extracts the allocated id from bullseye commit output.
// Looks for patterns like "ids: T110" or "🎯T110" or "T110".
func parseBullseyeTrackID(out string) string {
	// Prefer explicit ids: line from structured header.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "ids:") {
			rest := strings.TrimSpace(line[4:])
			rest = strings.Trim(rest, "[]")
			fields := strings.FieldsFunc(rest, func(r rune) bool {
				return r == ',' || r == ' ' || r == '"'
			})
			for _, f := range fields {
				if isTargetID(f) {
					return strings.TrimPrefix(f, "🎯")
				}
			}
		}
	}
	// Fallback: first 🎯Tn or standalone Tn token.
	for _, tok := range strings.Fields(out) {
		tok = strings.Trim(tok, ".,;:()[]\"'")
		if strings.HasPrefix(tok, "🎯") {
			tok = strings.TrimPrefix(tok, "🎯")
		}
		if isTargetID(tok) {
			return tok
		}
	}
	return ""
}

func isTargetID(s string) bool {
	if len(s) < 2 || s[0] != 'T' {
		return false
	}
	for i, c := range s[1:] {
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' && i > 0 {
			continue
		}
		return false
	}
	return true
}
