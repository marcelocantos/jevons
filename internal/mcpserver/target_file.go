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

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

// registerTargetFileTool exposes light-path bullseye filing for 🎯T93/T95
// (target: asides). Runs `bullseye commit --op track` in a repo cwd.
// 🎯T222: near-duplicate open leaves attach to the existing 🎯 id (no second id).
func (s *Server) registerTargetFileTool() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_target_file",
			mcp.WithDescription("File a bullseye target in a repo ledger (light path for owner target: asides — 🎯T93/T95). Requires bullseye CLI on PATH. Returns the new 🎯 id (or attaches to an existing near-duplicate open leaf — 🎯T222). After success, confirm the id to the owner; the UI auto-closes the filing aside when it sees the confirmation marker. Set force=true to deliberately allocate a second id for a same-mission split."),
			mcp.WithString("cwd", mcp.Required(), mcp.Description("Repo directory containing bullseye.yaml (or parent to discover)")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Desired-state assertion (target name)")),
			mcp.WithString("acceptance", mcp.Description("Acceptance criterion (single string; more can be space-separated or repeated via context)")),
			mcp.WithString("context", mcp.Description("Optional context / why. Optional mission_key:FOO tags same-mission for dedupe (🎯T222).")),
			mcp.WithBoolean("force", mcp.Description("If true, skip near-duplicate attach and allocate a new id (deliberate split residual 🎯T222). Default false.")),
		),
		s.handleTargetFile,
	)
}

// loadOpenLeavesForFile discovers open leaves for dedupe. Tests may override.
var loadOpenLeavesForFile = targetfile.LoadOpenLeavesFromCwd

func (s *Server) handleTargetFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	cwd := strings.TrimSpace(str(args["cwd"]))
	name := strings.TrimSpace(str(args["name"]))
	acceptance := strings.TrimSpace(str(args["acceptance"]))
	contextText := strings.TrimSpace(str(args["context"]))
	force := boolArg(args["force"])
	if cwd == "" || name == "" {
		return mcp.NewToolResultError("cwd and name are required"), nil
	}
	if acceptance == "" {
		acceptance = "Owner confirms the desired state is met."
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

	// 🎯T222: attach to existing open leaf when near-duplicate of name/acceptance/mission_key.
	missionKey := targetfile.ExtractMissionKey(contextText)
	if !force {
		if leaves, lerr := loadOpenLeavesForFile(abs); lerr == nil {
			if dup := targetfile.FindDuplicate(leaves, targetfile.Proposal{
				Name:       name,
				Acceptance: []string{acceptance},
				MissionKey: missionKey,
			}); dup != nil {
				return mcp.NewToolResultText(targetfile.AttachMessage(dup, name)), nil
			}
		}
		// Ledger missing / unreadable: fall through to track (no silent fail of filing).
	}

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
			"Target filed (parse id from output).\n__TARGET_FILED__:unknown\n\n%s", out)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Filed 🎯%s — %s\n__TARGET_FILED__:%s\n\n%s", id, name, id, out)), nil
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
