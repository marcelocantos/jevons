// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// t859ProbeScript writes argv0's trailing args and whatever it can read
// from stdin (with a short deadline, so a CLI that never touches stdin
// does not hang the test) to stdout as a small report the test parses.
const t859ProbeScript = `#!/bin/sh
printf 'ARGC=%d\n' "$#"
i=0
for a in "$@"; do
  i=$((i+1))
  printf 'ARG%d=%s\n' "$i" "$a"
done
`

func writeProbeScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.sh")
	if err := os.WriteFile(path, []byte(t859ProbeScript), 0o755); err != nil {
		t.Fatalf("write probe script: %v", err)
	}
	return path
}

// TestExecRunnerPassesPromptAsTrailingArgument is the T859-adjacent oracle
// for the 2026-09-23 finding: grok's headless `-p`/`--single <PROMPT>`
// requires the prompt as a CLI argument and does not read stdin at all
// ("a value is required for '--single <PROMPT>' but none was supplied").
// claude's `[options] [prompt]` also accepts it positionally, so appending
// the prompt as the trailing argument is correct for both, and covers the
// production DefaultCommand shape (`<cli> -p [--model model] {prompt}`).
func TestExecRunnerPassesPromptAsTrailingArgument(t *testing.T) {
	script := writeProbeScript(t)
	r := ExecRunner{}
	a := Assignment{
		Command: []string{script, "-p", "-m", "grok-4.5"},
		Prompt:  "the rendered auditor prompt\nwith a newline",
		Timeout: 30 * time.Second,
	}
	out, err := r.RunAudit(context.Background(), a)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	got := string(out.Raw)
	if !strings.Contains(got, "ARGC=4\n") {
		t.Fatalf("expected 4 trailing args (command's own 3 plus the appended prompt), got:\n%s", got)
	}
	if !strings.Contains(got, "ARG4="+a.Prompt+"\n") {
		t.Fatalf("expected the prompt appended as the trailing argument, got:\n%s", got)
	}
	if !strings.Contains(got, "ARG1=-p\n") || !strings.Contains(got, "ARG2=-m\n") || !strings.Contains(got, "ARG3=grok-4.5\n") {
		t.Fatalf("expected the configured command args ahead of the prompt, got:\n%s", got)
	}
}

// TestExecRunnerNoCommandConfigured guards the early-return path: an empty
// Command must fail loudly, not run an empty argv.
func TestExecRunnerNoCommandConfigured(t *testing.T) {
	r := ExecRunner{}
	_, err := r.RunAudit(context.Background(), Assignment{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "no command configured") {
		t.Fatalf("expected a no-command error, got: %v", err)
	}
}

// TestExecRunnerWrapsNonZeroExitWithStderr reproduces the live 2026-09-23
// symptom shape: a CLI that exits non-zero with a stderr complaint must
// surface that stderr in the returned error, not just the exit status.
func TestExecRunnerWrapsNonZeroExitWithStderr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fail.sh")
	script := "#!/bin/sh\necho \"a value is required for '--single <PROMPT>' but none was supplied\" >&2\nexit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fail script: %v", err)
	}
	r := ExecRunner{}
	_, err := r.RunAudit(context.Background(), Assignment{
		Command: []string{path},
		Prompt:  "x",
		Timeout: 30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected an error from a non-zero exit")
	}
	if !strings.Contains(err.Error(), "--single <PROMPT>") {
		t.Fatalf("expected stderr surfaced in the error, got: %v", err)
	}
}

// TestExecRunnerEmptyOutputIsAnError guards against a silent-success false
// green: a CLI that exits 0 with nothing on stdout must not be read as a
// clean (findings-free) audit pass.
func TestExecRunnerEmptyOutputIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write empty script: %v", err)
	}
	r := ExecRunner{}
	_, err := r.RunAudit(context.Background(), Assignment{
		Command: []string{path},
		Prompt:  "x",
		Timeout: 30 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "empty output") {
		t.Fatalf("expected an empty-output error, got: %v", err)
	}
}

// TestExecRunnerIgnoresStdin locks in the fix's other half: stdin is left
// disconnected (null device), matching a CLI that never reads it. A probe
// that tries to read stdin and times out proves the child does not block
// waiting for input that production never supplies.
func TestExecRunnerIgnoresStdin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readstdin.sh")
	// read -t bounds the read so a hang here fails fast instead of hanging
	// the whole test suite if the fix regresses and stdin is reconnected
	// to something that never closes.
	script := "#!/bin/sh\nif IFS= read -r -t 2 line; then\n  printf 'GOT:%s\\n' \"$line\"\nelse\n  printf 'NOINPUT\\n'\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write readstdin script: %v", err)
	}
	r := ExecRunner{}
	out, err := r.RunAudit(context.Background(), Assignment{
		Command: []string{path},
		Prompt:  "x",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	if got := string(out.Raw); !strings.Contains(got, "NOINPUT") {
		t.Fatalf("expected the child to see no stdin input, got: %q", got)
	}
}
