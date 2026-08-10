// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

func cfg() supervise.Config {
	return supervise.Config{
		Grace:           90 * time.Second,
		RetryInterval:   2 * time.Minute,
		MaxAttempts:     3,
		BackoffInterval: 10 * time.Minute,
	}
}

// TestServingIsQuiet: a healthy probe must neither act nor accumulate
// state, or every cycle would leave something behind to clean up.
func TestServingIsQuiet(t *testing.T) {
	d, st := supervise.Decide(cfg(), supervise.State{}, true, time.Now())
	if d.Action != supervise.ActionNone || d.Notify != supervise.NotifyNone {
		t.Fatalf("healthy probe acted: %+v", d)
	}
	if st.Down() || st.Attempts != 0 {
		t.Fatalf("healthy probe left state behind: %+v", st)
	}
}

// TestGraceHoldsBeforeActing is the bound the target asks for: a bounce in
// flight has a window, and only past it does an outage become the
// supervisor's problem. A grace shorter than a legitimate restart would
// fire a second restart into the middle of the first, every time.
func TestGraceHoldsBeforeActing(t *testing.T) {
	c := cfg()
	t0 := time.Date(2026, 8, 10, 23, 54, 0, 0, time.UTC)

	d, st := supervise.Decide(c, supervise.State{}, false, t0)
	if d.Action != supervise.ActionNone {
		t.Fatalf("first unserved observation acted immediately: %+v", d)
	}
	if !st.Down() || !st.DownSince.Equal(t0) {
		t.Fatalf("first unserved observation did not open an outage: %+v", st)
	}

	// Inside the window: still nothing.
	d, st = supervise.Decide(c, st, false, t0.Add(c.Grace-time.Second))
	if d.Action != supervise.ActionNone {
		t.Fatalf("acted inside the grace window: %+v", d)
	}
	if st.Attempts != 0 {
		t.Fatalf("counted an attempt inside the grace window: %+v", st)
	}

	// Past it: restart, and tell the owner once.
	d, st = supervise.Decide(c, st, false, t0.Add(c.Grace+time.Second))
	if d.Action != supervise.ActionRestart {
		t.Fatalf("did not restart past the grace window: %+v", d)
	}
	if d.Notify != supervise.NotifyProblem {
		t.Fatalf("first restart did not notify: %+v", d)
	}
	if st.Attempts != 1 || !st.Notified {
		t.Fatalf("restart not recorded: %+v", st)
	}
}

// TestOneNoticePerOutage: the supervisor runs every 30s, so a notice per
// cycle would make a five-minute outage cost ten alerts and train the
// owner to ignore them.
func TestOneNoticePerOutage(t *testing.T) {
	c := cfg()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	st := supervise.State{DownSince: now.Add(-2 * c.Grace)}

	d, st := supervise.Decide(c, st, false, now)
	if d.Action != supervise.ActionRestart || d.Notify != supervise.NotifyProblem {
		t.Fatalf("first restart: %+v", d)
	}
	notices := 1
	for i := 1; i <= 2; i++ {
		now = now.Add(c.RetryInterval + time.Second)
		d, st = supervise.Decide(c, st, false, now)
		if d.Action != supervise.ActionRestart {
			t.Fatalf("attempt %d did not restart: %+v", i+1, d)
		}
		if d.Notify != supervise.NotifyNone {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("outage cost %d notices before escalation, want 1", notices)
	}
}

// TestRetryPacingAndEscalation: attempts are paced, and when the outage
// outlives MaxAttempts the owner is told a second and final time that
// restarting is not taking — the point at which this stops being
// something the machinery can fix alone.
func TestRetryPacingAndEscalation(t *testing.T) {
	c := cfg()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	st := supervise.State{DownSince: now.Add(-2 * c.Grace)}

	var escalations int
	for i := 0; i < c.MaxAttempts; i++ {
		var d supervise.Decision
		d, st = supervise.Decide(c, st, false, now)
		if d.Action != supervise.ActionRestart {
			t.Fatalf("attempt %d: %+v", i+1, d)
		}
		// Immediately after an attempt, nothing more happens.
		if d2, _ := supervise.Decide(c, st, false, now.Add(time.Second)); d2.Action != supervise.ActionNone {
			t.Fatalf("attempt %d was not paced: %+v", i+1, d2)
		}
		now = now.Add(c.RetryInterval + time.Second)
	}
	if st.Attempts != c.MaxAttempts {
		t.Fatalf("attempts=%d want %d", st.Attempts, c.MaxAttempts)
	}

	// Past MaxAttempts the pacing widens to the backoff interval …
	if d, _ := supervise.Decide(c, st, false, now); d.Action != supervise.ActionNone {
		t.Fatalf("kept retrying at the fast interval past MaxAttempts: %+v", d)
	}
	// … and never stops: an outage nobody can fix now is still an outage
	// an hour later, when someone lands the fix.
	now = st.LastAttempt.Add(c.BackoffInterval + time.Second)
	d, st := supervise.Decide(c, st, false, now)
	if d.Action != supervise.ActionRestart {
		t.Fatalf("gave up restarting after MaxAttempts: %+v", d)
	}
	if !d.Escalated || d.Notify != supervise.NotifyProblem {
		t.Fatalf("did not escalate when restarts stopped taking: %+v", d)
	}
	escalations++
	if !st.Escalated {
		t.Fatalf("escalation not recorded: %+v", st)
	}

	// And escalates exactly once.
	for range 3 {
		now = now.Add(c.BackoffInterval + time.Second)
		d, st = supervise.Decide(c, st, false, now)
		if d.Notify != supervise.NotifyNone {
			escalations++
		}
	}
	if escalations != 1 {
		t.Fatalf("escalated %d times, want 1", escalations)
	}
}

// TestRecoveryClosesTheOutage: serving again clears the state, reports how
// long it was down, and tells the owner only about an outage they were
// told about in the first place.
func TestRecoveryClosesTheOutage(t *testing.T) {
	c := cfg()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	// An outage the owner heard about.
	st := supervise.State{DownSince: now.Add(-5 * time.Minute), Attempts: 2, Notified: true}
	d, next := supervise.Decide(c, st, true, now)
	if d.Action != supervise.ActionRecovered {
		t.Fatalf("recovery not detected: %+v", d)
	}
	if d.Notify != supervise.NotifyOK {
		t.Fatalf("recovery from a reported outage said nothing: %+v", d)
	}
	if d.Downtime != 5*time.Minute {
		t.Fatalf("downtime=%s want 5m", d.Downtime)
	}
	if next.Down() || next.Attempts != 0 || next.Notified {
		t.Fatalf("recovery did not clear state: %+v", next)
	}

	// A blip that resolved inside the grace window is not news, but it is
	// still a closed outage the daemon can report after the fact.
	st = supervise.State{DownSince: now.Add(-10 * time.Second)}
	d, _ = supervise.Decide(c, st, true, now)
	if d.Action != supervise.ActionRecovered {
		t.Fatalf("short outage not closed: %+v", d)
	}
	if d.Notify != supervise.NotifyNone {
		t.Fatalf("short unreported outage raised a notice: %+v", d)
	}
}
