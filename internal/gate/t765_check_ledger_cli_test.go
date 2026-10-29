// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T765: `gate check-ledger` is the ledger attestation equivalent of
// `gate check` + the FALSE-GREEN banner on finish reports. These run the
// shipped binary against a throwaway git repo holding the ledger — never the
// live ledger — with cwd in the jevons tree.

func checkLedger(t *testing.T, store string, r t765Repo, extra ...string) (int, string) {
	t.Helper()
	args := []string{"check-ledger", "-ledger", r.ledger}
	args = append(args, extra...)
	return runGate(t, store, args...)
}

func writeT765Ledger(t *testing.T, r t765Repo, body string) {
	t.Helper()
	if err := os.WriteFile(r.ledger, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Acceptance table (a)(b)(c) through the real check-ledger command, not only
// CheckAchieve in-process.
func TestCheckLedgerAcceptanceThroughCLI(t *testing.T) {
	r := newT765Repo(t, t765DateMixed)
	store := t.TempDir()
	fix := r.fix[:10]
	saveT765Record(t, store, "aaaa1111", r.kid, gate.VerdictGreen, 0, true)
	saveT765Record(t, store, "bbbb2222", r.kid, gate.VerdictDirty, 0, false)
	saveT765Record(t, store, "dddd4444", r.parent, gate.VerdictGreen, 0, true)

	writeT765Ledger(t, r, fmt.Sprintf(`targets:
  Tgood:
    status: achieved
    achieved: 2026-09-21
    attestation: 'Fixed in %s. GATE t9 exit=0 GREEN id=aaaa1111'
  Tprose:
    status: achieved
    achieved: 2026-09-21
    attestation: 'Fixed in %s; bin/gate -clean go test (GREEN).'
  Tdirty:
    status: achieved
    achieved: 2026-09-21
    attestation: 'Fixed in %s. GATE t9 exit=0 GREEN id=bbbb2222'
  Tancestor:
    status: achieved
    achieved: 2026-09-21
    attestation: 'Fixed in %s. GATE t9 exit=0 GREEN id=dddd4444'
  Thist:
    status: converging
    context: |-
      Achieved 2026-09-21: %s; bin/gate -clean go test (GREEN).

      Reverted 2026-09-21: false green.
`, fix, fix, fix, fix, fix))

	code, out := checkLedger(t, store, r)
	if code != 4 {
		t.Fatalf("live refusals: exit %d, want 4\n%s", code, out)
	}
	for _, needle := range []string{"Tprose", "achieve_gate_uncited", "Tdirty", "Tancestor", "achieve_gate_precedes_fix"} {
		if !strings.Contains(out, needle) {
			t.Errorf("output missing %q:\n%s", needle, out)
		}
	}
	if !strings.Contains(out, "Tgood") || !strings.Contains(out, "verified") {
		t.Errorf("Tgood should verify:\n%s", out)
	}
	if !strings.Contains(out, "historical achieves") || !strings.Contains(out, "Thist") {
		t.Errorf("historical section missing Thist:\n%s", out)
	}

	code, out = checkLedger(t, store, r, "-id", "Tgood")
	if code != 0 || !strings.Contains(out, "Tgood") || !strings.Contains(out, "verified") {
		t.Fatalf("-id Tgood: exit %d, want 0\n%s", code, out)
	}

	code, out = checkLedger(t, store, r, "-id", "Tprose")
	if code != 4 || !strings.Contains(out, "achieve_gate_uncited") {
		t.Fatalf("-id Tprose: exit %d, want 4\n%s", code, out)
	}
}

func TestCheckLedgerCannotJudgeIsNotAPass(t *testing.T) {
	store := t.TempDir()
	notGit := t.TempDir()
	led := filepath.Join(notGit, "bullseye.yaml")
	if err := os.WriteFile(led, []byte("targets:\n  T9:\n    status: achieved\n    attestation: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ := runGate(t, store, "check-ledger", "-ledger", led)
	if code != 70 {
		t.Fatalf("ledger outside git: exit %d, want 70", code)
	}
}
