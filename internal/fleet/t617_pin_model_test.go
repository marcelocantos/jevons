// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T617. Model switching belongs to claudia — the owner had SetModel and
// CapabilityModelSwitch built there deliberately, to fix this class in one
// place. jevons was never moved onto it, and kept doing stop-then-relaunch.
//
// Reproduced on the running daemon 2026-09-01: pinning jevons-po from
// grok-4.5 to grok-4 returned
//
//	pin model "jevons-po": relaunch: launch agent "jevons-po":
//	acp session/load 01a05c6d…: Path not found.
//
// and left the seat running=false with the registry claiming grok-4. Three
// consequences of one line, all of which these tests pin:
//
//   - a relaunch must re-attach to the existing conversation, so a seat
//     whose session will not load cannot be switched at all;
//   - Stop ran before Launch with no restore, so a failed switch converted
//     a healthy seat into a stopped one;
//   - the model reached the registry through the failed Launch.
//
// The source is asserted rather than the behaviour where a live agent would
// be needed: constructing one requires a real provider process, and a test
// that cannot run is worse than one that reads the code it guards.

func pinModelSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("pinmodel.go")
	if err != nil {
		t.Fatalf("read pinmodel.go: %v", err)
	}
	return string(body)
}

// The capability path must be tried before anything destructive happens.
func TestPinModelPrefersTheLiveSwitch(t *testing.T) {
	src := pinModelSource(t)
	set := strings.Index(src, "live.SetModel(model)")
	stop := strings.Index(src, "f.reg.Stop(name)")
	if set < 0 {
		t.Fatal("PinModel does not call SetModel — the capability exists and must be used (🎯T617)")
	}
	if stop < 0 {
		t.Fatal("the relaunch fallback is gone; providers without the capability still need it")
	}
	if set > stop {
		t.Fatalf("SetModel is attempted AFTER Stop (%d > %d): the destructive path must be the fallback, not the default", set, stop)
	}
}

// A failed switch must not leave the seat worse than it found it. The
// reported bug was a running seat becoming a stopped one.
func TestFailedRelaunchRestoresTheSeat(t *testing.T) {
	src := pinModelSource(t)
	i := strings.Index(src, "f.reg.Stop(name)")
	if i < 0 {
		t.Fatal("no fallback path to check")
	}
	tail := src[i:]
	if !strings.Contains(tail, "prev") || !strings.Contains(tail, "restor") {
		t.Fatalf("the fallback stops the seat and never restores it on failure:\n%s", tail[:min(len(tail), 700)])
	}
}

// "Cannot" and "tried and failed" are different, and only the first may
// fall through to the destructive path. A supported switch that failed is a
// real failure and must surface as one.
func TestOnlyAnUnsupportedProviderFallsBackToRelaunch(t *testing.T) {
	capErr := &claudia.CapabilityError{}
	if !isUnsupportedCapability(capErr) {
		t.Fatal("a CapabilityError must read as unsupported")
	}
	if !isUnsupportedCapability(fmt.Errorf("wrapped: %w", capErr)) {
		t.Fatal("a wrapped CapabilityError must still read as unsupported")
	}
	if isUnsupportedCapability(errors.New("acp session/load: Path not found")) {
		t.Fatal("a genuine failure was treated as 'provider cannot do this' — that is how a healthy seat gets stopped")
	}
	if isUnsupportedCapability(nil) {
		t.Fatal("nil is not an unsupported capability")
	}
}

// The registry must not learn a model the seat never reached.
func TestModelIsRecordedOnlyAfterTheSwitchSucceeds(t *testing.T) {
	src := pinModelSource(t)
	ok := strings.Index(src, "case err == nil:")
	rec := strings.Index(src, "switched.Model = model")
	if ok < 0 || rec < 0 {
		t.Fatal("cannot locate the success path that records the model")
	}
	if rec < ok {
		t.Fatal("the model is recorded outside the success branch — a failed switch would relabel the row")
	}
	// And the session must be left alone: Register only resets resume
	// state when SessionID changes, so the copy must not touch it.
	between := src[ok:]
	if strings.Contains(between[:min(len(between), 600)], "SessionID =") {
		t.Fatal("the success path rewrites SessionID — that discards resume state for a mere model change")
	}
}
