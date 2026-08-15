// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T453. 🎯T441 closed the hole where an unknown *subcommand* was executed and
// minted a citable green; this is the surviving half — arguments a *known*
// subcommand cannot honour. The tool did the part it understood and exited 0,
// so the caller cited the output of a command that had not done what they
// asked. These drive the shipped binary, because the argument policy is a
// property of what a worker types, not of a package API.

// checkReport is a finish report whose own cited evidence contradicts the pass
// it claims: the status quoted is tail's, not the suite's. `gate check` must
// flag it — which is also how a test proves the report was *read* rather than
// quietly skipped in favour of an empty stdin.
const checkReport = "🎯T453 finished, tests pass.\n\n" +
	"    $ go test ./... 2>&1 | tail -20\n" +
	"    --- FAIL: TestSomething (0.02s)\n" +
	"    $ echo \"exit=$?\"\n    exit=0\n"

func writeReport(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte(checkReport), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Acceptance 1. The form a reader types after seeing a report on disk. It used
// to ignore the path and block on stdin with nothing printed — which under a
// background gate is indistinguishable from a hung suite, and cost 120s and a
// harness timeout in the session that found it.
func TestT453CheckReadsAPathArgument(t *testing.T) {
	store := t.TempDir()
	path := writeReport(t)

	code, out := runGate(t, store, "check", path)
	if code == 0 {
		t.Fatalf("gate check %s exited 0; the report it names is a false green "+
			"and must be flagged (the path was ignored)\n%s", path, out)
	}
	if code != 4 {
		t.Fatalf("gate check exited %d, want 4 (flagged)\n%s", code, out)
	}
	if !strings.Contains(out, string(gate.FlagPipelineMasked)) {
		t.Fatalf("gate check did not flag the piped status in the named file; "+
			"it did not read it:\n%s", out)
	}

	// An honest report at a path is equally read, and stays quiet.
	honest := filepath.Join(t.TempDir(), "honest.md")
	if err := os.WriteFile(honest, []byte("Still working; go test is running."), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runGate(t, store, "check", honest); code != 0 {
		t.Fatalf("gate check flagged an honest report at a path: exit %d\n%s", code, out)
	}

	// A path that is not there is an error naming it, not silence and not a
	// fallback to stdin — falling back is how the original defect read as a
	// hang rather than as a typo.
	code, out = runGate(t, store, "check", filepath.Join(t.TempDir(), "absent.md"))
	if code == 0 {
		t.Fatalf("gate check on a missing file exited 0:\n%s", out)
	}
	if !strings.Contains(out, "absent.md") {
		t.Fatalf("gate check did not name the file it could not read:\n%s", out)
	}
}

// Acceptance 3's bounded-time clause, and the property that actually failed in
// the field: with a path argument, gate must not touch stdin at all. The test
// hands it a pipe that never reaches EOF — the shape of the harness's own
// stdin — so a binary that reads stdin here hangs until the deadline kills it,
// exactly as it did live.
func TestT453CheckWithAPathDoesNotBlockOnStdin(t *testing.T) {
	store := t.TempDir()
	path := writeReport(t)

	// The write end stays open for the whole test, so the child's stdin is a
	// pipe with a writer and no data: a read blocks indefinitely.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	const budget = 20 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	cmd := exec.CommandContext(ctx, gateBinary(t), "check", path)
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = r
	out, err := cmd.CombinedOutput()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("gate check %s blocked on stdin past %s and was killed; a named "+
			"path must be read without waiting on stdin\n%s", path, budget, out)
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("running gate check: %v\n%s", err, out)
	}
	if code := cmd.ProcessState.ExitCode(); code != 4 {
		t.Fatalf("gate check exited %d, want 4 (the named report is a false green)\n%s",
			code, out)
	}
}

// Acceptance 2. Each of these performed the part it understood and exited 0 (or
// blamed the store for what the command line got wrong), discarding the rest.
func TestT453SurplusArgumentIsRefusedAndNamed(t *testing.T) {
	store := t.TempDir()

	// A real record, so that the honoured forms have something to answer with
	// and a silently-ignored argument would otherwise still print a green.
	if code, out := runGate(t, store, "-name", "demo", "-quiet", "--", "true"); code != 0 {
		t.Fatalf("seeding a record failed: exit %d\n%s", code, out)
	}
	_, seeded := runGate(t, store, "last")
	id := recordID(t, seeded)

	for _, tc := range []struct {
		name    string
		args    []string
		offence string
	}{
		// The one observed live: printed the newest record and exited 0.
		{"last with a surplus argument", []string{"last", "extra-junk-arg"}, "extra-junk-arg"},
		{"show with a surplus argument", []string{"show", id, "extra-junk-arg"}, "extra-junk-arg"},
		{"check with a surplus path", []string{"check", "a.md", "b.md"}, "b.md"},
		// Ignored the operand and swept the whole store, which is the command
		// that decides what stops being citable.
		{"sweep with a stray operand", []string{"sweep", "stray-operand"}, "stray-operand"},
		{"sweep with an unknown flag", []string{"sweep", "-nope"}, "nope"},
		// Read the flag as a record id and failed with "no such record" —
		// blaming the store for a command-line mistake.
		{"void with an unknown flag", []string{"void", "-nope", id, "reason"}, "nope"},
		{"show with an unknown flag", []string{"show", "-nope"}, "nope"},
		{"check with an unknown flag", []string{"check", "-nope"}, "nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runGate(t, store, tc.args...)
			if code == 0 {
				t.Fatalf("gate %s exited 0; an argument gate cannot honour must be "+
					"refused, not discarded\n%s", strings.Join(tc.args, " "), out)
			}
			if !strings.Contains(out, tc.offence) {
				t.Fatalf("gate %s did not name the offending argument %q; the reader "+
					"needs to be pointed at their own command line\n%s",
					strings.Join(tc.args, " "), tc.offence, out)
			}
			// A refused invocation is not a run: it must leave no evidence.
			if n := countRecords(t, store); n != 1 {
				t.Fatalf("gate %s changed the store (%d records, want the 1 seeded)",
					strings.Join(tc.args, " "), n)
			}
		})
	}
}

// The other half of any refusal: it must not have narrowed what still works.
func TestT453HonouredFormsAreUnchanged(t *testing.T) {
	store := t.TempDir()
	if code, out := runGate(t, store, "-name", "demo", "-quiet", "--", "true"); code != 0 {
		t.Fatalf("seeding a record failed: exit %d\n%s", code, out)
	}

	code, out := runGate(t, store, "last")
	if code != 0 || !strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("gate last no longer answers: exit %d\n%s", code, out)
	}
	id := recordID(t, out)

	if code, out := runGate(t, store, "show", id); code != 0 || !strings.Contains(out, id) {
		t.Fatalf("gate show %s no longer answers: exit %d\n%s", id, code, out)
	}
	if code, out := runGate(t, store, "sweep"); code != 0 {
		t.Fatalf("gate sweep on a clean store exited %d:\n%s", code, out)
	}

	// The stdin form the doc comment has always advertised, unchanged.
	cmd := exec.Command(gateBinary(t), "check")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = strings.NewReader(checkReport)
	stdinOut, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("gate check < report stopped flagging a false green:\n%s", stdinOut)
	}
	if code := cmd.ProcessState.ExitCode(); code != 4 {
		t.Fatalf("gate check < report exited %d, want 4\n%s", code, stdinOut)
	}
	if !strings.Contains(string(stdinOut), string(gate.FlagPipelineMasked)) {
		t.Fatalf("gate check < report lost the flag:\n%s", stdinOut)
	}

	// And the explicit `-` operand still means stdin rather than a filename.
	cmd = exec.Command(gateBinary(t), "check", "-")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = strings.NewReader(checkReport)
	dashOut, _ := cmd.CombinedOutput()
	if code := cmd.ProcessState.ExitCode(); code != 4 {
		t.Fatalf("gate check - exited %d, want 4 (stdin)\n%s", code, dashOut)
	}
}

// recordID pulls the id out of an attestation line, which is how a reader gets
// one to pass to `gate show`.
func recordID(t *testing.T, attestation string) string {
	t.Helper()
	for f := range strings.FieldsSeq(attestation) {
		if id, ok := strings.CutPrefix(f, "id="); ok {
			return id
		}
	}
	t.Fatalf("no id= in attestation:\n%s", attestation)
	return ""
}
