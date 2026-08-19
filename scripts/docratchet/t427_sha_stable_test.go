// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/shaevidence"
)

// TestT427DoctrineMarkers ratchets the corrected predicate and the ancestor
// check into every surface acceptance 6 names (🎯T427).
func TestT427DoctrineMarkers(t *testing.T) {
	need := []string{
		"🎯T427",
		"git merge-base --is-ancestor",
		"bullseye.yaml",
		"multi-file",
	}
	for _, doc := range []string{
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, doc)
		for _, m := range need {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing 🎯T427 doctrine marker %q", doc, m)
			}
		}
	}
}

// TestT427PredicateIsLedgerOnlyNotFileCount locks acceptance 1 in code: a
// single-file code path is safe; only bullseye.yaml alone is the amend shape.
func TestT427PredicateIsLedgerOnlyNotFileCount(t *testing.T) {
	if !shaevidence.TouchesOnlyLedger([]string{"bullseye.yaml"}) {
		t.Fatal("bullseye.yaml alone must be the amend-vulnerable path shape")
	}
	if shaevidence.TouchesOnlyLedger([]string{"src/graph.rs"}) {
		t.Fatal("single-file code commit must not be classified ledger-only")
	}
}

// TestT427ExtractIgnoresGateBookkeeping ensures gate id=/out= hex is not
// mistaken for evidence SHAs (same trap FlagFalseGreen already documents).
func TestT427ExtractIgnoresGateBookkeeping(t *testing.T) {
	text := "GATE t exit=0 GREEN id=9f13c0a2 out=6b1d9e4f2a01\nSHA abcdef1 lands it"
	got := shaevidence.ExtractEvidenceSHAs(text)
	if len(got) != 1 || got[0].SHA != "abcdef1" {
		t.Fatalf("got %#v, want only abcdef1", got)
	}
}

// TestT427AuditorDetectsRewrittenAndMissing is the hermetic half of acceptance
// 5: given a ledger-shaped attestation blob, the walk reports rewritten vs
// missing and stays silent on ancestors.
func TestT427AuditorDetectsRewrittenAndMissing(t *testing.T) {
	blob := strings.Join([]string{
		`    attestation: 'SHA aaa1111 lands the fix'`,
		`    attestation: 'SHA bbb2222 was cited then amended away'`,
		`      Achieved 2026-08-10: commit ccc3333 never existed here`,
	}, "\n")
	check := shaevidence.CheckFunc(func(sha string) shaevidence.Reachability {
		switch sha {
		case "aaa1111":
			return shaevidence.Ancestor
		case "bbb2222":
			return shaevidence.Rewritten
		case "ccc3333":
			return shaevidence.Missing
		default:
			return shaevidence.Missing
		}
	})
	got := shaevidence.ScanFindings(shaevidence.AttestationBlob(blob), check)
	if len(got) != 2 {
		t.Fatalf("got %#v, want rewritten+missing", got)
	}
	if got[0].SHA != "bbb2222" || got[0].Kind != shaevidence.Rewritten {
		t.Fatalf("first %#v", got[0])
	}
	if got[1].SHA != "ccc3333" || got[1].Kind != shaevidence.Missing {
		t.Fatalf("second %#v", got[1])
	}
}

// TestT427LiveInstancesStillRewritten locks acceptance 4's named SHAs: when
// the object still exists locally, CheckInRepo must classify it rewritten
// (not ancestor, not missing). Proves detection against the real repo.
func TestT427LiveInstancesStillRewritten(t *testing.T) {
	root := gitRepo(t)
	check := shaevidence.CheckInRepo(root, "HEAD")
	checked := 0
	for _, sha := range []string{"078fd53", "6055e2c", "df41fda"} {
		if err := exec.Command("git", "-C", root, "cat-file", "-e", sha+"^{commit}").Run(); err != nil {
			t.Logf("skip %s: not a commit object here", sha)
			continue
		}
		checked++
		if k := check(sha); k != shaevidence.Rewritten {
			t.Errorf("%s: got %s, want rewritten (🎯T427 live instance)", sha, k)
		}
	}
	if checked == 0 {
		t.Skip("none of the 🎯T427 live-instance SHAs remain as objects in this clone")
	}
}

// TestT427LedgerSHAAudit is the standing enforcement point (acceptance 5):
// walk bullseye.yaml attestations and REPORT non-ancestors. Historical
// unreachable citations are reported, never silently rewritten in place —
// so a non-zero rewritten count is information, not a suite failure.
func TestT427LedgerSHAAudit(t *testing.T) {
	root := gitRepo(t)
	raw, err := os.ReadFile(filepath.Join(root, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	blob := shaevidence.AttestationBlob(string(raw))
	if strings.TrimSpace(blob) == "" {
		t.Fatal("AttestationBlob(bullseye.yaml) was empty — no attestation prose found")
	}
	findings := shaevidence.ScanLedger(root, string(raw))
	var rewritten, missing int
	for _, f := range findings {
		switch f.Kind {
		case shaevidence.Rewritten:
			rewritten++
		case shaevidence.Missing:
			missing++
		}
	}
	t.Logf("🎯T427 ledger audit: %d rewritten, %d missing citations reported (not rewritten in place)",
		rewritten, missing)
}
