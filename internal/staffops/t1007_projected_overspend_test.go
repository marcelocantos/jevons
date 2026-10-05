// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"strings"
	"testing"
)

// 🎯T1007: reproduces the 🎯T851 shape for projected-overspend, but keyed
// off trustworthy accounting evidence (the monitor's own subscription
// marker in Detail) rather than a blanket suppression of the symptom name.

// t1007SubscriptionDetail mirrors internal/cost.Monitor's detail format
// under AccountingSubscription: usdLabel = "API-eq est USD (not billed)".
const t1007SubscriptionDetail = "projected 42.00 API-eq est USD (not billed) today (budget 30.00)"

// t1007BillableDetail mirrors the list_price accounting detail format:
// usdLabel = "USD" (no subscription marker) — real billable dollars.
const t1007BillableDetail = "projected 42.00 USD today (budget 30.00)"

func TestT1007SubscriptionProjectedOverspendIsNotAFilePOMission(t *testing.T) {
	d := Classify(Signal{
		Kind:     "cost_alert",
		Symptom:  "cost:projected-overspend",
		Severity: "medium",
		Detail:   t1007SubscriptionDetail,
	})
	if d.Action == ActionFilePO {
		t.Fatalf("subscription projected-overspend filed a PO mission: %s (%s)", d.Action, d.Reason)
	}
	if d.Action != ActionHarnessOK && d.Action != ActionIgnore {
		t.Fatalf("action=%s want harness-ok or ignore: %s", d.Action, d.Reason)
	}
	if !strings.Contains(d.Reason, "informational") {
		t.Errorf("reason does not name the informational path: %q", d.Reason)
	}
}

// High/critical severity must not override the informational path —
// observe marks CostAlert Mechanical:false, so without this special case
// medium+ becomes file+PO.
func TestT1007SubscriptionProjectedOverspendHighSeverityStillNotFilePO(t *testing.T) {
	for _, sev := range []string{"medium", "high", "critical"} {
		d := Classify(Signal{
			Kind:     "cost_alert",
			Symptom:  "cost:projected-overspend",
			Severity: sev,
			Detail:   t1007SubscriptionDetail,
		})
		if d.Action == ActionFilePO {
			t.Fatalf("severity=%s filed a PO mission: %s (%s)", sev, d.Action, d.Reason)
		}
	}
}

// The control a blanket (🎯T851-style, symptom-only) fix would fail: a
// billable projected-overspend — no subscription marker in Detail — is
// exactly the alarm the cost auditor exists to raise, and must still file.
func TestT1007BillableProjectedOverspendStillFilesPO(t *testing.T) {
	for _, sev := range []string{"medium", "high", "critical"} {
		d := Classify(Signal{
			Kind:     "cost_alert",
			Symptom:  "cost:projected-overspend",
			Severity: sev,
			Detail:   t1007BillableDetail,
		})
		if d.Action != ActionFilePO {
			t.Fatalf("billable severity=%s action=%s want file+PO: %s", sev, d.Action, d.Reason)
		}
	}
}

// A projected-overspend with no detail at all (fleet/collector safety:
// absence of accounting evidence is not evidence of subscription) still
// files — it is not silently swallowed just because Detail is empty.
func TestT1007ProjectedOverspendNoDetailStillFilesPO(t *testing.T) {
	d := Classify(Signal{
		Kind:     "cost_alert",
		Symptom:  "cost:projected-overspend",
		Severity: "medium",
	})
	if d.Action != ActionFilePO {
		t.Fatalf("action=%s want file+PO: %s", d.Action, d.Reason)
	}
}

// Other cost symptoms — collector-stale, fleet-rate — must still file
// regardless of accounting evidence appearing in their own detail text;
// the marker only excuses projected-overspend, never a different symptom.
func TestT1007OtherCostSymptomsStillFilePO(t *testing.T) {
	for _, tc := range []struct {
		symptom string
		detail  string
	}{
		{"cost:collector-stale", "cost collector has not polled since 2026-09-15T19:15:49+10:00 — burn figures may be blind"},
		{"cost:fleet-rate", "fleet burn 12.00 API-eq est USD (not billed)/hr (warn ≥ 10.00); 8 events"},
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

func TestT1007NamesProjectedOverspendMatchesWholeFieldsOnly(t *testing.T) {
	if !namesProjectedOverspend("cost:projected-overspend") {
		t.Error("expected cost:projected-overspend to match")
	}
	if namesProjectedOverspend("cost:fleet-rate") {
		t.Error("fleet-rate must not match projected-overspend")
	}
	if namesProjectedOverspend("projected-overspend-ish") {
		t.Error("partial field match must not count")
	}
}
