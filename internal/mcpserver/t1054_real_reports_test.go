// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

// These 20 committed report specimens were captured for earlier regressions,
// not invented to flatter this classifier. They are all prose-bearing or
// otherwise ambiguous. None is a labelled metadata-only routine report.
// The measured false-negative denominator is therefore zero, not a claimed
// 100% recall. Synthetic positive controls live in t1054_routine_metadata_test.
func TestT1054TwentyRealReportSpecimensFailOpen(t *testing.T) {
	specimens := []string{
		"testdata/t1024_arrai_t41_nest_quadratic_report.md",
		"testdata/t1024_arrai_t41_verify_land_report.md",
		"testdata/t395_jv_t387_stranded_report.md",
		"testdata/t439_jv_t435_brief_request_report.md",
		"testdata/t445_reaped_checkpoint_report.md",
		"testdata/t446_meta_mention_report.md",
		"testdata/t446_negated_mention_report.md",
		"testdata/t470_jv_t390_checkpoint_report.md",
		"testdata/t470_jv_t391_checkpoint_report.md",
		"testdata/t497_jv_t496_checkpoint_report.md",
		"testdata/t497_jv_t496_true_finish_report.md",
		"testdata/t497_jv_t498_midmission_report.md",
		"testdata/t497_jv_t498_sha_checkpoint_report.md",
		"testdata/t577_jv_t568_checkpoint_report.md",
		"testdata/t581_jv_t564_reading_done_report.md",
		"testdata/t716_jv_t702_checkpoint_report.md",
		"testdata/t736_scout_report_specimen.txt",
		"testdata/t946_jv_t906_accept_lifts_block_report.md",
		"testdata/t946_jv_t943_stale_token_reload_report.md",
		"../relayroute/testdata/t658/t657-scout-report.txt",
	}
	if len(specimens) != 20 {
		t.Fatalf("sample size %d", len(specimens))
	}
	s := &Server{registry: t1054Registry(t, "jevons-po")}
	falsePositive := 0
	for _, name := range specimens {
		text, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		if s.routineMetadataReport("worker", string(text)) {
			falsePositive++
			t.Errorf("real specimen %s incorrectly suppressed", name)
		}
	}
	t.Logf("real report precision sample: n=20, labelled routine=0, labelled owner-visible=20, false positives=%d, false negatives=not measurable (no routine positives)", falsePositive)
}

func TestT1054OwnerRepeatedSendNeverSuppressed(t *testing.T) {
	s, inbox := chainServer(t, nil)
	const repeated = "Can you show me status now?"
	for i := 0; i < 2; i++ {
		res, err := s.deliverByName(s.overseerName(), repeated, OriginOwner, false)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status == StatusSuppressedReplay {
			t.Fatal("owner repeat was deduplicated")
		}
	}
	if len(inbox.texts) != 2 {
		t.Fatalf("owner sends=%d want two", len(inbox.texts))
	}
}

// Owner ingress audit: terminal event notifyTurn is the only place with both
// the author's registry identity and a confirmed parent-report disposition.
// Direct MCP sends (including T392.7 reroutes), daemon notices and owner chat
// lack that proof; even a metadata-looking direct send must pass unchanged.
func TestT1054DirectOverseerSendNeverUsesRoutineSuppression(t *testing.T) {
	s, inbox := chainServer(t, nil)
	s.registry = t1054Registry(t, "jevons-po")
	for _, text := range []string{t1054Finish(), t1054Finish() + "\nUnexpected safety incident: relay stalled."} {
		if _, err := s.deliverByName(s.overseerName(), text, OriginAgent, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(inbox.texts) != 2 {
		t.Fatalf("direct sends reached owner: %d want 2", len(inbox.texts))
	}
}
