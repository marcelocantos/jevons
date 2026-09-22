// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// 🎯T854: a 🎯T708 load notice is the governor's own repair, not a residual
// product gap. Both arms are pinned here — the notice classifies harness-ok,
// and a real medium+ daemon_error still files.

// t854LoadNameLine is the specimen sentence, built by the formatter that
// actually writes it, so a rename of the verdict in internal/capacity fails
// here instead of silently unmatching the sentinel arm.
func t854LoadNameLine() string {
	return capacity.FormatLoadAction(capacity.LoadAction{
		Source: capacity.LoadSource{
			Seat:       "jv-t813-land",
			Procs:      41,
			CPUPercent: 16,
			Age:        8*time.Minute + 45*time.Second,
			Heaviest:   "go run ./cmd/envelopecheck /tmp/sup/t813-finish.md",
		},
		Verdict:  capacity.LoadName,
		Audience: capacity.AudienceSeat,
		Pressure: capacity.PressureElevated,
		Reason:   "host elevated; naming the heaviest seat-created load source to the seat that created it",
	})
}

func TestT854SeatLoadSymptomIsNotAFilePOMission(t *testing.T) {
	d := Classify(Signal{
		Kind:     "daemon_error",
		Symptom:  "event:error:seat_load",
		Severity: "high",
		Detail:   t854LoadNameLine(),
	})
	if d.Action == ActionFilePO {
		t.Fatalf("seat_load filed a PO mission: %s (%s)", d.Action, d.Reason)
	}
	if d.Action != ActionHarnessOK && d.Action != ActionIgnore {
		t.Fatalf("action=%s want harness-ok or ignore: %s", d.Action, d.Reason)
	}
	if !strings.Contains(d.Reason, "T708") {
		t.Errorf("reason does not name the repair that already happened: %q", d.Reason)
	}
}

// The notify_queue route: the component is not in the fingerprint, so the
// governor's own sentence in the detail has to carry it.
func TestT854LoadNameDetailOnNotifyQueueIsNotAFilePOMission(t *testing.T) {
	d := Classify(Signal{
		Kind:     "notify_queue",
		Symptom:  "notify_queue",
		Severity: "high",
		Detail:   t854LoadNameLine(),
	})
	if d.Action == ActionFilePO {
		t.Fatalf("load-name notify_queue filed a PO mission: %s (%s)", d.Action, d.Reason)
	}
}

// The control an over-broad fix fails: silencing daemon_error wholesale
// would pass both arms above and kill the alarm the sentinel exists for.
func TestT854RealDaemonErrorStillFilesPO(t *testing.T) {
	for _, sev := range []string{"medium", "high", "critical"} {
		d := Classify(Signal{
			Kind:     "daemon_error",
			Symptom:  "event:error:butler",
			Severity: sev,
			Detail:   "panic: send on closed channel",
		})
		if d.Action != ActionFilePO {
			t.Fatalf("severity=%s action=%s want file+PO: %s", sev, d.Action, d.Reason)
		}
	}
}

// A load line that is not the NAME verdict must not be matched by the detail
// route alone — only the component fingerprint speaks for those.
func TestT854DetailRouteMatchesTheNameVerdictOnly(t *testing.T) {
	terminate := capacity.FormatLoadAction(capacity.LoadAction{
		Source: capacity.LoadSource{
			Seat: "jv-quiet", Procs: 9, CPUPercent: 380,
			Age: time.Hour, Orphaned: 9, SeatIdle: true,
			Heaviest: "while :; do go test -race; done",
		},
		Verdict:  capacity.LoadTerminate,
		Audience: capacity.AudienceOwner,
		Pressure: capacity.PressureCritical,
	})
	if looksLikeLoadNameLine(terminate) {
		t.Fatalf("terminate line matched the name-verdict route: %s", terminate)
	}
	if !looksLikeLoadNameLine(t854LoadNameLine()) {
		t.Fatal("name line did not match its own route")
	}
}

func TestT854NamesSeatLoadMatchesWholeFieldsOnly(t *testing.T) {
	if !namesSeatLoad("event:error:seat_load") {
		t.Error("component fingerprint not recognized")
	}
	if namesSeatLoad("event:error:seat_load_estimator") {
		t.Error("a different component was swallowed by a prefix match")
	}
	if namesSeatLoad("daemon_error: seat_load balancing regression in butler") {
		t.Error("prose mentioning the component was read as the governor speaking")
	}
}

// Cycle level: the notice does not become a filed symptom or a spawn mission.
func TestT854CycleDoesNotSpawnForALoadNotice(t *testing.T) {
	res := RunCycle(CycleArgs{
		Signals: []Signal{{
			Kind:     "daemon_error",
			Symptom:  "event:error:seat_load",
			Severity: "high",
			Detail:   t854LoadNameLine(),
		}},
		Sentinel: true,
		DryRun:   true,
		Now:      time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC),
	})
	if len(res.FiledSymptoms) != 0 {
		t.Fatalf("filed symptoms = %v, want none", res.FiledSymptoms)
	}
	if res.Primary == ActionFilePO {
		t.Fatalf("primary = %s, want no file+PO", res.Primary)
	}
	if strings.Contains(res.WireText, "Act: deliver mission to jevons-po") {
		t.Fatalf("load notice delivered a PO mission:\n%s", res.WireText)
	}
}
