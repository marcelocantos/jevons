// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package bouncetiming_test

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/bouncetiming"
)

func TestT794ParseSpecimenLog(t *testing.T) {
	const log = `
time=2026-09-22T02:18:14.000+10:00 level=INFO msg="🎯T191 restart-jevonsd: root=/jevons port=13705"
time=2026-09-22T02:19:21.000+10:00 level=INFO msg="shutting down" signal=hangup exit_mode=upgrade
time=2026-09-22T02:19:23.099+10:00 level=INFO msg="loaded CA from disk" dir=/Users/x/.jevons
time=2026-09-22T02:19:32.802+10:00 level=INFO msg="cockpit: converge loop started" interval=3s
`
	events, err := bouncetiming.Parse(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	w, ok := bouncetiming.LastBounceWindows(events)
	if !ok {
		t.Fatal("expected windows from specimen log")
	}
	want := 9*time.Second + 703*time.Millisecond
	if d := w.ProcessToConverge.Round(time.Millisecond); d != want {
		t.Fatalf("process→converge %v want %v", d, want)
	}
	if w.Shutdown.IsZero() {
		t.Fatal("expected old shutdown before new process")
	}
	healthAt := time.Date(2026, 9, 22, 2, 19, 35, 0, w.ProcessStart.Location())
	down := bouncetiming.ShutdownToHealth(w, healthAt)
	if down != 14*time.Second {
		t.Fatalf("shutdown→health %v want 14s", down)
	}
	procOK, downOK := bouncetiming.WithinLimits(w, down)
	if procOK {
		t.Fatalf("specimen process→converge %.1fs should exceed 5s bar", w.ProcessToConverge.Seconds())
	}
	if downOK {
		t.Fatalf("specimen downtime %.1fs exceeds 8s bar (why pre_shutdown split matters)", down.Seconds())
	}
}

func TestT794WithinLimitsFastBoot(t *testing.T) {
	t0 := time.Now()
	w := bouncetiming.Windows{
		ProcessStart:      t0,
		ConvergeStart:     t0.Add(4 * time.Second),
		ProcessToConverge: 4 * time.Second,
	}
	procOK, downOK := bouncetiming.WithinLimits(w, 6*time.Second)
	if !procOK || !downOK {
		t.Fatalf("expected within limits proc=%v down=%v", procOK, downOK)
	}
}
