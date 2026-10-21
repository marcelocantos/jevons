// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T766.4: an unenveloped sentence is not a completion class. Envelope
// slots still are.
func TestClassifyCompletionReport(t *testing.T) {
	for _, in := range []string{
		"",
		"still reading the tree",
		"Done. Mission complete.",
		"All finished, ready for review.",
		"Done. SHA abcdef0123456. go test ./internal/mcpserver -run T31 PASS",
		"complete — make test-web green",
		"Oracle: node web/scripts/foo_test.js PASS",
		"Done with accepted risk: owner smoke residual (class-3)",
		"Residual: isolated class-3 human gate for visual feel",
		"accepted-risk: no hermetic path for this spike",
		"finished; accepted risk logged",
		"done; hermetic TestEnsureFleetBriefInjectsOnce green",
	} {
		if got := ClassifyCompletionReport(in); got != CompletionNoClaim {
			t.Fatalf("ClassifyCompletionReport(%q)=%s want no_claim", in, got)
		}
	}
	if got := ClassifyCompletionReport(declaredFinishReport("T31", "Landed.")); got != CompletionOracleEvidence {
		t.Fatalf("declared finish class=%s", got)
	}
}

func TestLooksLikeBareDoneAndHasOracleOrRisk(t *testing.T) {
	if LooksLikeBareDone("Done, nothing else.") {
		t.Fatal("unenveloped prose is not a bare done")
	}
	if LooksLikeBareDone("Done. go test ./... PASS") {
		t.Fatal("unenveloped oracle prose is not a bare done")
	}
	if HasOracleOrRisk("SHA deadbeef make test green") {
		t.Fatal("unenveloped oracle prose is not oracle evidence")
	}
	if HasOracleOrRisk("accepted risk residual for owner") {
		t.Fatal("unenveloped risk prose is not accepted risk")
	}
	if HasOracleOrRisk("done") {
		t.Fatal("bare done is not oracle-or-risk")
	}
	if !HasOracleOrRisk(declaredFinishReport("T31", "Landed.")) {
		t.Fatal("declared finish with oracle slots must be oracle evidence")
	}
}

func TestCompletionEvidenceClassString(t *testing.T) {
	if CompletionBareDone.String() != "bare_done" {
		t.Fatalf("got %q", CompletionBareDone.String())
	}
	if CompletionOracleEvidence.String() != "oracle_evidence" {
		t.Fatalf("got %q", CompletionOracleEvidence.String())
	}
}

// 🎯T194 / 🎯T766.4: activation is the envelope daily slot. A sentence that
// names a restart, a curl, or the development surface is not that slot.
func TestHasActivationEvidence(t *testing.T) {
	activated := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T194",
		SHA:          "abcdef0123456",
		Daily:        "restart-jevonsd",
		Verdict:      envelope.VerdictGreen,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "observed on the development surface",
	})
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"hermetic only", "Done. SHA abcdef0. go test ./internal/server -run Frontier PASS", false},
		{"make test only", "complete — make test green", false},
		{"restart sentence", "Done. Detached restart-jevonsd succeeded; SHA deadbeef", false},
		{"live curl sentence", "Live probe: curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:13705/api/frontier → 200", false},
		{"http 200 frontier sentence", "GET http://127.0.0.1:13705/api/frontier → HTTP 200 with targets JSON", false},
		{"non-404 sentence", "restart-jevonsd exit 0; /api/frontier non-404", false},
		{"daily path sentence", "proven on the daily path after bounce", false},
		{"development path sentence", "proven on the development path after bounce", false},
		{"development surface sentence", "observed on the development surface", false},
		{"zero-downtime sentence", "proven zero-downtime upgrade; product path green", false},
		{"status only", "still rebuilding", false},
		{"declared oracle without daily", declaredFinishReport("T194", "go test ./internal/server PASS"), false},
		{"declared daily slot", activated, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasActivationEvidence(tc.in)
			if got != tc.want {
				t.Fatalf("HasActivationEvidence(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooksLikeHermeticOnlyDaemonClaim(t *testing.T) {
	prose := "Done. hermetic go test ./internal/server PASS"
	if LooksLikeHermeticOnlyDaemonClaim(prose) {
		t.Fatal("a hermetic sentence is not a daemon claim")
	}
	if !LooksLikeHermeticOnlyDaemonClaim(declaredFinishReport("T194", prose)) {
		t.Fatal("a declared hermetic finish with no activation slot must flag")
	}
	activated := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T194",
		SHA:          "abcdef0123456",
		Daily:        "restart-jevonsd",
		Verdict:      envelope.VerdictGreen,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "curl /api/frontier HTTP 200",
	})
	if LooksLikeHermeticOnlyDaemonClaim(activated) {
		t.Fatal("a declared daily slot must clear the hermetic-only flag")
	}
	if LooksLikeHermeticOnlyDaemonClaim("Done. restart-jevonsd + curl /api/frontier HTTP 200. go test green") {
		t.Fatal("a restart sentence is not activation and is not a hermetic-only claim")
	}
	if LooksLikeHermeticOnlyDaemonClaim("still reading the tree") {
		t.Fatal("non-claim must not flag")
	}
	if LooksLikeHermeticOnlyDaemonClaim("Done. Mission complete.") {
		t.Fatal("bare done without an oracle slot is not a hermetic-only daemon claim")
	}
}

func TestT430LiveRenderOracleDidNotRun(t *testing.T) {
	if LiveRenderOracleDidNotRun("Live-render oracle DID NOT RUN: Browser is already in use for profile") {
		t.Fatal("a sentence is not a browser-oracle admission")
	}
	if LiveRenderOracleDidNotRun("Done. go test ./internal/mcpattach -run T430 PASS") {
		t.Fatal("plain hermetic finish is not a browser non-result")
	}
}

func TestT430LooksLikeSilentBrowserOracleFallback(t *testing.T) {
	silent := "Browser is already in use for /Users/marcelo/.config/playwright-mcp/profile; cited go test ./internal/ui PASS as live evidence"
	if LooksLikeSilentBrowserOracleFallback(silent) {
		t.Fatal("hermetic cited after a browser sentence is not a silent fallback")
	}
	honest := "Browser is already in use for playwright-mcp/profile. Live-render oracle DID NOT RUN. No hermetic substitution."
	if LooksLikeSilentBrowserOracleFallback(honest) {
		t.Fatal("an honest non-result sentence is not a silent fallback")
	}
	clean := "Done. SHA abcdef0. go test ./internal/mcpattach -run T430 PASS"
	if LooksLikeSilentBrowserOracleFallback(clean) {
		t.Fatal("no browser block — not a fallback")
	}
}
