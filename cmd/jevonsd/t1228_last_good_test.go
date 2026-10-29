// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// A broker restart drops the link for seconds. Last-good readings stay on
// the bars (they render neutral and age by their own timestamps); only a
// replica that has never observed a provider reports disconnected (🎯T1228).
func TestServedPlanUsageKeepsLastGoodThroughLinkDrop(t *testing.T) {
	raw := json.RawMessage(`{"backends":[{"provider":"claude","status":"available"}]}`)
	observed := []planusage.Backend{{Provider: "claude", FetchedAt: time.Unix(1_800_000_000, 0)}}
	if got := servedPlanUsage(false, raw, observed); string(got) != string(raw) {
		t.Fatalf("link drop replaced last-good readings: %s", got)
	}
	if got := servedPlanUsage(false, raw, []planusage.Backend{{Provider: "claude"}}); string(got) != string(planUsageDisconnected) {
		t.Fatalf("never-observed replica while disconnected served %s", got)
	}
	if got := servedPlanUsage(true, raw, nil); string(got) != string(raw) {
		t.Fatalf("linked replica not served as-is: %s", got)
	}
}
