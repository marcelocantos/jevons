// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

// 🎯T693 acceptance as filed 2026-09-20 — names product land and live mint evidence.
var t693Acceptance = []string{
	"Before any product land: document (in finish-report) where mint/migrate/park ranking **actually executes** after T691 (`ee6e7b02`) — jevons vs claudia call graph with file/func cites.",
	"Band-first routing (`under` outranks `ok`; pressure tiebreak within same band; hot/ahead never destinations) is implemented in **one home** on the selection side of the seam — claudia if placement resolves there; not duplicated in jevons.",
	"Fixture — tonight's owner case: Claude weekly under (~71%, ~17h) vs Grok weekly ok (~5%, ~137h) → pick Claude despite more negative Grok pressure.",
	"Per-model weekly at 0% (Fable) does not veto provider when other Claude models eligible.",
	"Hermetic through the production path on the chosen side; fleet-brief: Fable spent ≠ Claude unavailable.",
}

const t693Attestation1 = "SHA 5acbcdaa27b5. GATE go-test-T693T495UnknownT285_2WeeklyBand exit=0 GREEN id=bf6a17a8 tree=dirty+9@5acbcdaa — internal/planusage GREEN. PickPlanDest ranks published band first: under before ok; hot/ahead never dest; pressure is tiebreak. Dirty files are bullseye.yaml and screenshot PNGs, not T693 sources. Residual: T397 screenshots/ledger dirty; no stored finish-report (T690); daemon not rebuilt (T176). Local only."

const t693Attestation2 = "SHA 140b525dc2b9. GATE go-test-180s exit=0 GREEN id=35c2b8e1 tree=dirty+12@140b525dc2b9 — internal/planusage and internal/mcpserver GREEN. Band-first ranking lives in claudia.Resolve (T691 seam); PickMintDest no longer ranks. Fixture TestT693BandFirstClaudeUnderBeatsGrokOk despite Grok pressure. Dirty files are T390.1.2 UI (PlanUsageBar.tsx, pace.ts) and screenshot PNGs, not T693 sources. Residual: T397 sibling WIP; ranking implementation is in claudia (pin via ee6e7b02/T691); daemon not rebuilt (T176). Local only."

func t703Lookup(id string) (*Record, bool) {
	return &Record{
		ID: id, Name: "t693", StatusKnown: true, ExitStatus: 0, Verdict: VerdictGreen,
		Tree: &TreeProvenance{Commit: "5acbcdaa27b5", Clean: true},
	}, true
}

func TestT703RejectsT693HistoricalAttestations(t *testing.T) {
	for name, att := range map[string]string{
		"first achieve 2026-09-20":  t693Attestation1,
		"second achieve 2026-09-20": t693Attestation2,
	} {
		t.Run(name, func(t *testing.T) {
			r := CheckAchieve(&AchieveCheckArgs{
				Attestation: att,
				Acceptance:  t693Acceptance,
				Lookup:      t703Lookup,
			})
			if r.Verdict != AchieveRefused {
				t.Fatalf("verdict %q, want refused: %s", r.Verdict, r)
			}
			var product Flag
			for _, f := range r.Flags {
				if f.Kind == FlagAchieveProductPathUnrun {
					product = f
					break
				}
			}
			if product.Kind == "" {
				t.Fatalf("no achieve_product_path_unrun flag in: %v", r.Flags)
			}
			if !strings.Contains(product.Detail, "daemon not rebuilt") {
				t.Fatalf("detail missing daemon not rebuilt: %q", product.Detail)
			}
			if !strings.Contains(r.Detailed(), "field=acceptance") {
				t.Fatalf("detailed missing field=acceptance: %s", r.Detailed())
			}
		})
	}
}

func TestT703HermeticAcceptanceUnaffected(t *testing.T) {
	acceptance := []string{
		"Hermetic: go test ./internal/gate -run TestFoo -count=1 passes.",
	}
	att := "GATE go-test exit=0 GREEN id=abcd1234. Residual: daemon not rebuilt (T176). Local only."
	r := CheckAchieve(&AchieveCheckArgs{
		Attestation: att,
		Acceptance:  acceptance,
		Lookup:      t703Lookup,
	})
	if r.Verdict != AchieveVerified {
		t.Fatalf("verdict %q, want verified: %s", r.Verdict, r)
	}
}

func TestT703ProductAchieveWithObservationPasses(t *testing.T) {
	acceptance := []string{"Owner-visible on hard-reloaded cockpit after activation."}
	att := "GATE ui exit=0 GREEN id=abcd1234. Activation independently confirmed: curl http://localhost:13705/health → build matches HEAD."
	r := CheckAchieve(&AchieveCheckArgs{
		Attestation: att,
		Acceptance:  acceptance,
		Lookup:      t703Lookup,
	})
	if r.Verdict != AchieveVerified {
		t.Fatalf("verdict %q, want verified: %s", r.Verdict, r)
	}
}

func TestT703AcceptanceOverrideSkipsRefusal(t *testing.T) {
	acceptance := []string{"Live evidence: seat minted on claude while grok is ok."}
	att := "GATE go-test exit=0 GREEN id=abcd1234. daemon not rebuilt. achieve_acceptance_override: acceptance mis-filed; hermetic-only target."
	r := CheckAchieve(&AchieveCheckArgs{
		Attestation: att,
		Acceptance:  acceptance,
		Lookup:      t703Lookup,
	})
	if r.Verdict != AchieveVerified {
		t.Fatalf("verdict %q, want verified with override: %s", r.Verdict, r)
	}
}
