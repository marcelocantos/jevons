// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/treeguard"
)

func TestLogBranchCheckoutDenialAppendsEvent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JEVONS_STATE_DIR", dir)
	t.Setenv(commitattrib.ActorEnv, "jv-t955-auto")

	treeguard.LogBranchCheckoutDenial(treeguard.Decision{
		Verdict: treeguard.Deny,
		Reason:  "branch-switch-in-shared-clone",
		Message: "treeguard: refusing to check out master in the shared clone — its checked-out branch is the integration branch steer. 🎯T955",
	})

	path := eventlog.DefaultPath(dir)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(b))
	var ev eventlog.Event
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Component != "treeguard" || ev.Decision != "branch-checkout-refused" {
		t.Fatalf("event %+v", ev)
	}
	if ev.Fields["seat"] != "jv-t955-auto" {
		t.Fatalf("fields %+v", ev.Fields)
	}
	if !strings.Contains(ev.Msg, "T955") {
		t.Fatalf("message should cite T955: %q", ev.Msg)
	}
}
