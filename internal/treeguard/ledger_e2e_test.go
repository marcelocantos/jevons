package treeguard_test

// 🎯T546: a mutating hook on bullseye.yaml is refused and names the
// bullseye tool. The binary path is the Claude PreToolUse contract
// (exit 2 + stderr). Cursor StrReplace is covered in claudia.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookBinaryDeniesStrReplaceOfLedger(t *testing.T) {
	bin := guardBinary(t)
	repo := t.TempDir()
	onDisk := []byte("schema_version: 5\ntargets: {}\n")
	ledger := filepath.Join(repo, "bullseye.yaml")
	if err := os.WriteFile(ledger, onDisk, 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"session_id": "t546",
		"cwd":        repo,
		"tool_name":  "StrReplace",
		"tool_input": map[string]any{
			"file_path": ledger,
			"content":   "schema_version: 5\ntargets: {T1: {}}\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "pre")
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"CLAUDE_PROJECT_DIR="+repo,
		"JEVONS_TREEGUARD_DIR="+t.TempDir(),
	)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err = cmd.Run()
	var exitErr *exec.ExitError
	if err == nil {
		t.Fatal("treeguard pre allowed StrReplace of bullseye.yaml")
	}
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("exit = %v stderr=%s, want 2", err, errBuf.String())
	}
	stderr := errBuf.String()
	if !strings.Contains(stderr, "jevons_target_file") || !strings.Contains(stderr, "T546") {
		t.Fatalf("refuse must name the bullseye tool:\n%s", stderr)
	}
	got, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(onDisk) {
		t.Fatalf("ledger mutated after refuse:\n%s", got)
	}
}
