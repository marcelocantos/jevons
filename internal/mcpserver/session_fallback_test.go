// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
)

func TestSessionFallbackSeedSkipsThePlanWall(t *testing.T) {
	recs := []agentreport.Record{
		{ID: "1", At: time.Date(2026, 9, 23, 6, 22, 0, 0, time.UTC), Text: strings.Repeat("The frontier is T189 and the pins stay on v3.1.1. ", 3)},
		{ID: "2", At: time.Date(2026, 9, 23, 10, 31, 0, 0, time.UTC), Text: "Upgrade your plan to continue"},
	}
	got := sessionFallbackSeed("multimaze2-po", "82615afb-238d-44bd-a9fa-407b43009f13", recs)
	if !strings.Contains(got, "82615afb") || !strings.Contains(got, "not there") || !strings.Contains(got, "new session") {
		t.Fatalf("seed does not report the missing session: %s", got)
	}
	if !strings.Contains(got, "v3.1.1") {
		t.Fatalf("seed dropped the stored report: %s", got)
	}
	if strings.Contains(got, "Upgrade your plan") {
		t.Fatalf("plan wall was treated as context: %s", got)
	}
}

func TestSessionFallbackSeedEmptyWithoutSubstance(t *testing.T) {
	recs := []agentreport.Record{{Text: "Upgrade your plan to continue"}, {Text: "ok"}}
	if got := sessionFallbackSeed("a", "sid", recs); got != "" {
		t.Fatalf("seed = %q", got)
	}
}
