// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

func blockedFinish() *Message {
	return &Message{
		Kind:         KindFinishReport,
		Target:       "T935",
		Status:       ProgressBlocked,
		Blocker:      "daemon-restart-needs-owner-go-ahead",
		SilentLedger: SilentLedgerEmpty,
		Payload:      "Implementation is ready; activation needs the owner's go-ahead.",
	}
}

// TestT938BlockedFinishReportRoundTrips: acceptance 1 — a finish-report can
// say status blocked plus the blocker it waits on, and parse round-trips it.
func TestT938BlockedFinishReportRoundTrips(t *testing.T) {
	raw := Format(blockedFinish())
	for _, want := range []string{"jevons: status blocked\n", "jevons: blocker daemon-restart-needs-owner-go-ahead\n"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("formatted envelope missing %q:\n%s", want, raw)
		}
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, raw)
	}
	if got.Status != ProgressBlocked || got.Blocker != "daemon-restart-needs-owner-go-ahead" {
		t.Fatalf("status=%q blocker=%q", got.Status, got.Blocker)
	}
	if !got.IsBlocked() {
		t.Fatal("IsBlocked = false for status blocked + blocker")
	}
	if Format(got) != raw {
		t.Fatalf("round trip drifted:\n%s\n---\n%s", raw, Format(got))
	}
	if b, ok := BlockedOn(raw); !ok || b != "daemon-restart-needs-owner-go-ahead" {
		t.Fatalf("BlockedOn = %q, %v", b, ok)
	}
	if got.Status.ProductVisible() {
		t.Fatal("blocked must not be a product-visible status")
	}
}

// TestT938QualifiedBlockedStatusParses: the 2026-09-30 incident spelling
// (status=blocked_design) reads as blocked; the reason rides the blocker slot.
func TestT938QualifiedBlockedStatusParses(t *testing.T) {
	for _, raw := range []string{"blocked", "Blocked", "blocked_design", "blocked-on-owner", "blocked on owner"} {
		p, ok := ParseProgress(raw)
		if !ok || p != ProgressBlocked {
			t.Errorf("ParseProgress(%q) = %q, %v; want blocked", raw, p, ok)
		}
	}
	if _, ok := ParseProgress("blockedx"); ok {
		t.Error("ParseProgress(blockedx) accepted a non-status")
	}
}

// TestT938BlockedNeedsBlockerNotOracle: a blocked finish-report claims no
// completion, so it owes no oracle — but it must name what it waits on.
func TestT938BlockedNeedsBlockerNotOracle(t *testing.T) {
	m := blockedFinish()
	if err := Validate(m); err != nil {
		t.Fatalf("blocked finish-report with blocker and no oracle: %v", err)
	}
	m.Blocker = ""
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "blocker") {
		t.Fatalf("blocked finish-report without blocker: err=%v, want a blocker complaint", err)
	}
	if _, ok := BlockedOn(Format(m)); ok {
		t.Fatal("BlockedOn true for a blocked status with no named blocker")
	}
	// A non-blocked finish-report still owes an oracle or risk.
	done := blockedFinish()
	done.Status = ProgressInProgress
	if err := Validate(done); err == nil {
		t.Fatal("non-blocked finish-report with no oracle validated")
	}
	if _, ok := BlockedOn(Format(done)); ok {
		t.Fatal("BlockedOn true for a non-blocked finish-report")
	}
}

// TestT938BlockedOnOnlyForFinishReports: a status-ping saying blocked is a
// mid-work note, not the stored terminal declaration the sweeps honour.
func TestT938BlockedOnOnlyForFinishReports(t *testing.T) {
	ping := Format(&Message{Kind: KindStatusPing, Status: ProgressBlocked, Blocker: "owner"})
	if _, err := Parse(ping); err != nil {
		t.Fatalf("status-ping blocked: %v", err)
	}
	if _, ok := BlockedOn(ping); ok {
		t.Fatal("BlockedOn true for a status-ping")
	}
	quoted := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: status blocked\njevons: blocker \"owner go-ahead on restart\"\njevons: silent-ledger none\n```\n"
	if b, ok := BlockedOn(quoted); !ok || b != "owner go-ahead on restart" {
		t.Fatalf("quoted blocker = %q, %v", b, ok)
	}
}
