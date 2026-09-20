// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/seatactivity"
)

const t745Diff = "diff --git a/internal/x_test.go b/internal/x_test.go\n+func TestFoo(t *testing.T) {\n+\tt.Fatal(1)\n"

func t745Err(reason, frame string) error {
	return errors.New("claude not ready (" + reason + "): no idle input box after 30s; last frame:\n" + frame)
}

func t745Known(age time.Duration) seatactivity.Reading {
	return seatactivity.Reading{Verdict: seatactivity.VerdictKnown, Age: age}
}

func TestT745PaneBusyRendering(t *testing.T) {
	unknown := seatactivity.Reading{Verdict: seatactivity.VerdictUnknown}
	cases := []struct {
		name     string
		err      error
		act      seatactivity.Reading
		reminted bool
		want     bool
	}{
		{"specimen: diff on screen, transcript 2.5s old", t745Err("no_composer", t745Diff), t745Known(2500 * time.Millisecond), false, true},
		{"never started: no transcript", t745Err("no_composer", t745Diff), unknown, false, false},
		{"stale transcript is a wedge, not busy", t745Err("no_composer", t745Diff), t745Known(10 * time.Minute), false, false},
		{"blank frame draws nothing", t745Err("no_composer", "  \n"), t745Known(time.Second), false, false},
		{"splash is positive startup evidence", t745Err("splash", t745Diff), t745Known(time.Second), false, false},
		{"rc_connecting never overridden", t745Err("rc_connecting", t745Diff), t745Known(time.Second), false, false},
		{"bounce-reminted seat keeps old mtime", t745Err("no_composer", t745Diff), t745Known(time.Second), true, false},
		{"not a stall at all", errors.New("boom"), t745Known(time.Second), false, false},
		{"nil", nil, t745Known(time.Second), false, false},
	}
	for _, c := range cases {
		if got := paneBusyRendering(c.err, c.act, c.reminted); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// The wrapped owner copy still classifies as a stall (🎯T729 round trip), so
// the class survives agent_send's "send failed: %s" wrap; only the seat's
// transcript decides busy vs stalled.
func TestT745ClassSurvivesSendWrap(t *testing.T) {
	_, owner := agenterr.ClassifyAndFormat(t745Err("no_composer", t745Diff))
	wrapped := errors.New("send failed: " + owner)
	if got := agenterr.ClassifyText(wrapped.Error()); got != agenterr.ClassStartupStall {
		t.Fatalf("class evaporated across the wrap: %v", got)
	}
}
