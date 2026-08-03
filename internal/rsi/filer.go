// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BullseyeFiler shells out to the bullseye CLI (same path as jevons_target_file).
type BullseyeFiler struct {
	// Run is optional override for tests (args → stdout, err).
	Run func(args ...string) (string, error)
}

// File implements Filer via `bullseye commit --op track`.
// 🎯T226: always allocates a new id (near-dup file attach removed).
func (f BullseyeFiler) File(args FileArgs) (string, error) {
	cwd := strings.TrimSpace(args.Cwd)
	name := strings.TrimSpace(args.Name)
	if cwd == "" || name == "" {
		return "", fmt.Errorf("rsi filer: cwd and name required")
	}
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[2:])
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("rsi filer: cwd: %w", err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("rsi filer: cwd is not a directory: %s", abs)
	}
	acceptance := args.Acceptance
	if len(acceptance) == 0 {
		acceptance = []string{"Owner confirms the desired state is met."}
	}

	cmdArgs := []string{
		"commit", "--op", "track",
		"--cwd", abs,
		"--name", name,
	}
	for _, a := range acceptance {
		a = strings.TrimSpace(a)
		if a != "" {
			cmdArgs = append(cmdArgs, "--acceptance", a)
		}
	}
	if ctx := strings.TrimSpace(args.Context); ctx != "" {
		cmdArgs = append(cmdArgs, "--context", ctx)
	}
	// Tag origin for ledger archaeology.
	cmdArgs = append(cmdArgs, "--origin", "ambient-rsi")

	run := f.Run
	if run == nil {
		run = runBullseye
	}
	out, err := run(cmdArgs...)
	if err != nil {
		return "", fmt.Errorf("bullseye commit: %w\n%s", err, out)
	}
	id := parseBullseyeTrackID(out)
	if id == "" {
		return "", fmt.Errorf("bullseye commit: no id in output:\n%s", out)
	}
	return id, nil
}

func runBullseye(args ...string) (string, error) {
	cmd := exec.Command("bullseye", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// parseBullseyeTrackID extracts the allocated id from bullseye commit output.
func parseBullseyeTrackID(out string) string {
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
