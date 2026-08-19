// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package briefaddr

import (
	"errors"
	"strings"
	"testing"
)

func TestIdentityHeaderNameReadsHeaderBlockOnly(t *testing.T) {
	header := Marker + " — from the fleet registry, read at send time (🎯T425)]\n" +
		"- NAME: jevons-po\n" +
		"- ROLE: product owner\n" +
		"- PARENT: jevons\n\n" +
		"Continue the mission.\n" +
		"- NAME: someone-else\n"
	if got := IdentityHeaderName(header); got != "jevons-po" {
		t.Fatalf("IdentityHeaderName = %q, want jevons-po", got)
	}
	if got := IdentityHeaderName("plain note\n- NAME: not-a-header\n"); got != "" {
		t.Fatalf("headerless text returned %q", got)
	}
	quoted := "Incident report:\n" + header
	if got := IdentityHeaderName(quoted); got != "" {
		t.Fatalf("quoted header deeper in the body answered %q", got)
	}
}

func TestCheckWrongSeatVsOwnSeatVsHeaderless(t *testing.T) {
	poBrief := Marker + "]\n- NAME: jevons-po\n\nContinue."
	ownBrief := Marker + "]\n- NAME: jevons\n\nStanding brief."
	note := "worker jv-x reports: tests green"

	if err := Check("jevons", poBrief); err == nil {
		t.Fatal("wrong-seat brief for overseer seat was accepted")
	} else {
		var m *MisaddressedBriefError
		if !errors.As(err, &m) {
			t.Fatalf("want *MisaddressedBriefError, got %T: %v", err, err)
		}
		if m.Addressed != "jevons-po" || m.Destination != "jevons" {
			t.Fatalf("error names = %+v", m)
		}
		if !strings.Contains(err.Error(), "🎯T452") {
			t.Fatalf("typed error does not name T452: %v", err)
		}
	}
	if err := Check("jevons-po", poBrief); err != nil {
		t.Fatalf("correctly addressed brief refused: %v", err)
	}
	if err := Check("jevons", ownBrief); err != nil {
		t.Fatalf("overseer-addressed brief refused: %v", err)
	}
	if err := Check("jevons", note); err != nil {
		t.Fatalf("headerless note refused: %v", err)
	}
}
