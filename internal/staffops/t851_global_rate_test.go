// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"strings"
	"testing"
	"time"
)

// 🎯T851: a sentinel cycle does not open a file+PO mission for an
// informational global-rate cost alert. Both arms are pinned here — the
// subscription global burn classifies harness-ok, and a medium+ collector-stale
// or fleet-rate still files.

// t851SpecimenDetail is the 2026-09-22 T219 cycle that prescribed file+PO.
const t851SpecimenDetail = "global burn 10.64 API-eq est USD (not billed)/hr, warn >= 10, 1 event"

func TestT851GlobalRateIsNotAFilePOMission(t *testing.T) {
	d := Classify(Signal{
		Kind:     "cost_alert",
		Symptom:  "cost:global-rate",
		Severity: "medium",
		Detail:   t851SpecimenDetail,
	})
	if d.Action == ActionFilePO {
		t.Fatalf("informational global-rate filed a PO mission: %s (%s)", d.Action, d.Reason)
	}
	if d.Action != ActionHarnessOK && d.Action != ActionIgnore {
		t.Fatalf("action=%s want harness-ok or ignore: %s", d.Action, d.Reason)
	}
	if !strings.Contains(d.Reason, "informational") {
		t.Errorf("reason does not name the enforcer's informational path: %q", d.Reason)
	}
}

// High/critical severity must not override the informational path — observe
// marks CostAlert Mechanical:false, so without this special case medium+
// becomes file+PO.
func TestT851GlobalRateHighSeverityStillNotFilePO(t *testing.T) {
	for _, sev := range []string{"medium", "high", "critical"} {
		d := Classify(Signal{
			Kind:     "cost_alert",
			Symptom:  "cost:global-rate",
			Severity: sev,
			Detail:   t851SpecimenDetail,
		})
		if d.Action == ActionFilePO {
			t.Fatalf("severity=%s filed a PO mission: %s (%s)", sev, d.Action, d.Reason)
		}
	}
}

// The control an over-broad fix fails: silencing cost_alert wholesale would
// pass the global-rate arm and kill the alarm the sentinel exists for.
func TestT851CollectorStaleAndFleetRateStillFilePO(t *testing.T) {
	for _, tc := range []struct {
		symptom string
		detail  string
	}{
		{"cost:collector-stale", "cost collector has not polled since 2026-09-15T19:15:49+10:00 — burn figures may be blind"},
		{"cost:fleet-rate", "fleet burn 12.00 USD/hr (warn ≥ 10.00); 8 events"},
	} {
		for _, sev := range []string{"medium", "high", "critical"} {
			d := Classify(Signal{
				Kind:     "cost_alert",
				Symptom:  tc.symptom,
				Severity: sev,
				Detail:   tc.detail,
			})
			if d.Action != ActionFilePO {
				t.Fatalf("%s severity=%s action=%s want file+PO: %s", tc.symptom, sev, d.Action, d.Reason)
			}
		}
	}
}

func TestT851NamesGlobalRateMatchesWholeFieldsOnly(t *testing.T) {
	if !namesGlobalRate("cost:global-rate") {
		t.Error("cost:global-rate fingerprint not recognized")
	}
	if namesGlobalRate("cost:collector-stale") {
		t.Error("collector-stale was swallowed as global-rate")
	}
	if namesGlobalRate("cost:fleet-rate") {
		t.Error("fleet-rate was swallowed as global-rate")
	}
	if namesGlobalRate("cost:global-rate-estimator") {
		t.Error("a different kind was swallowed by a prefix match")
	}
	if namesGlobalRate("daemon_error: global-rate accounting regression in butler") {
		t.Error("prose mentioning the kind was read as the enforcer speaking")
	}
}

func TestT851InformationalGlobalRateRequiresCostAlertKind(t *testing.T) {
	if !InformationalGlobalRate(Signal{Kind: "cost_alert", Symptom: "cost:global-rate"}) {
		t.Fatal("cost_alert + cost:global-rate must match")
	}
	if InformationalGlobalRate(Signal{Kind: "frontier_stall", Symptom: "cost:global-rate"}) {
		t.Fatal("a non-cost kind must not be swallowed by the symptom fingerprint")
	}
	if InformationalGlobalRate(Signal{Kind: "cost_alert", Symptom: "cost:fleet-rate"}) {
		t.Fatal("fleet-rate must not match")
	}
}

// Cycle level: the notice does not become a filed symptom or a spawn mission.
func TestT851CycleDoesNotSpawnForGlobalRate(t *testing.T) {
	res := RunCycle(CycleArgs{
		Signals: []Signal{{
			Kind:     "cost_alert",
			Symptom:  "cost:global-rate",
			Severity: "medium",
			Detail:   t851SpecimenDetail,
		}},
		Sentinel: true,
		DryRun:   true,
		Now:      time.Date(2026, 9, 22, 8, 45, 0, 0, time.UTC),
	})
	if len(res.FiledSymptoms) != 0 {
		t.Fatalf("filed symptoms = %v, want none", res.FiledSymptoms)
	}
	if res.Primary == ActionFilePO {
		t.Fatalf("primary = %s, want no file+PO", res.Primary)
	}
	if strings.Contains(res.WireText, "Act: deliver mission to jevons-po") {
		t.Fatalf("global-rate delivered a PO mission:\n%s", res.WireText)
	}
}

// Observe still emits the signal (awareness stays on the wire) but Classify
// does not file.
func TestT851ObserveThenClassifyGlobalRateIsNotFilePO(t *testing.T) {
	sigs := BuildSignals(ObserveInput{
		OverseerAlive: true,
		CostAlerts: []CostObs{{
			Kind:     "global-rate",
			Severity: "medium",
			Detail:   t851SpecimenDetail,
		}},
	})
	var got *Signal
	for i := range sigs {
		if sigs[i].Kind == "cost_alert" && sigs[i].Symptom == "cost:global-rate" {
			got = &sigs[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("observe must still emit cost:global-rate; got %+v", sigs)
	}
	d := Classify(*got)
	if d.Action == ActionFilePO {
		t.Fatalf("observe→classify filed a PO mission: %s (%s)", d.Action, d.Reason)
	}
}
