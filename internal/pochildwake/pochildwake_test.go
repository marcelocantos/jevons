// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package pochildwake_test

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/pochildwake"
)

func TestUnactedRequiresReportAfterLastTurn(t *testing.T) {
	t0 := time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC)
	po := pochildwake.POObs{Name: "jevons-po", Alive: true, Phase: "idle", GraceElapsed: true, LastTurnEnded: t0}
	reports := []pochildwake.Report{{
		Agent: "jv-w", Target: "T1", Kind: "finish-report", ReportID: "r1",
		StoredAt: t0.Add(time.Minute),
	}}
	got := pochildwake.Unacted(po, reports, nil)
	if len(got) != 1 {
		t.Fatalf("want 1 unacted, got %d", len(got))
	}
	po.LastTurnEnded = t0.Add(2 * time.Minute)
	if len(pochildwake.Unacted(po, reports, nil)) != 0 {
		t.Fatal("report before last turn end must not count")
	}
}

func TestClassifyWakeAfterGrace(t *testing.T) {
	po := pochildwake.POObs{Name: "jevons-po", Alive: true, Phase: "idle", GraceElapsed: true}
	pending := []pochildwake.Report{{Agent: "jv-w", Target: "T1193", Kind: "scout-report", ReportID: "abc"}}
	v, detail := pochildwake.Classify(po, pending)
	if v != pochildwake.VerdictWake {
		t.Fatalf("verdict=%s want wake", v)
	}
	if !pochildwake.IsFault(v) {
		t.Fatal("wake must be fault")
	}
	for _, want := range []string{"jv-w", "scout-report", "abc"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail=%q missing %q", detail, want)
		}
	}
}
