// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poauditor_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/poauditor"
)

func TestClassifyMutualWaitAfterGrace(t *testing.T) {
	po := poauditor.POObs{Name: "jevons-po", Alive: true, Phase: "idle", GraceElapsed: true}
	aud := poauditor.AuditorObs{Name: "jv-auditor-po", Alive: true, Phase: "idle", QueueDepth: 0}
	h := []poauditor.Handoff{{Agent: "jv-t1191-auto", Target: "T1191"}}
	v, detail := poauditor.Classify(po, aud, h)
	if v != poauditor.VerdictMutualWait {
		t.Fatalf("verdict=%q want mutual_wait", v)
	}
	if !poauditor.IsFault(v) {
		t.Fatal("mutual_wait must be a fault")
	}
	for _, want := range []string{"jevons-po", "jv-auditor-po", "jv-t1191-auto", "T1191"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail=%q missing %q", detail, want)
		}
	}
}

func TestClassifyWithinGrace(t *testing.T) {
	po := poauditor.POObs{Name: "jevons-po", Alive: true, Phase: "idle", GraceElapsed: false}
	aud := poauditor.AuditorObs{Name: "jv-auditor-po", Alive: true, Phase: "idle"}
	h := []poauditor.Handoff{{Agent: "w", Target: "T1"}}
	if v, _ := poauditor.Classify(po, aud, h); v != poauditor.VerdictWithinGrace {
		t.Fatalf("verdict=%q want within_grace", v)
	}
}

func TestClassifyAuditorQueued(t *testing.T) {
	po := poauditor.POObs{Name: "jevons-po", Alive: true, Phase: "idle", GraceElapsed: true}
	aud := poauditor.AuditorObs{Name: "jv-auditor-po", Alive: true, Phase: "idle", QueueDepth: 1}
	h := []poauditor.Handoff{{Agent: "w", Target: "T1"}}
	if v, _ := poauditor.Classify(po, aud, h); v != poauditor.VerdictAuditorBusy {
		t.Fatalf("verdict=%q want auditor_busy", v)
	}
}
