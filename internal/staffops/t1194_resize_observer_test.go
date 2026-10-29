// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"testing"
	"time"
)

// 🎯T1194: benign ResizeObserver loop warnings must not cluster or file+PO.

func t1194BenignRows(ts time.Time) []EventRow {
	msg := "ResizeObserver loop completed with undelivered notifications."
	return []EventRow{
		{Level: "error", Msg: "window.onerror", Component: "window", ErrorMessage: msg, TS: ts},
		{Level: "error", Msg: "window.onerror", Component: "window", ErrorMessage: msg, TS: ts.Add(time.Second)},
	}
}

func TestT1194BenignResizeObserverNotClustered(t *testing.T) {
	now := time.Date(2026, 10, 22, 14, 31, 0, 0, time.UTC)
	got := ClusterEventAnomalies(t1194BenignRows(now), now, 15*time.Minute)
	if len(got) != 0 {
		t.Fatalf("clustered benign ResizeObserver rows: %+v", got)
	}
}

func TestT1194BenignResizeObserverClassifyIgnores(t *testing.T) {
	d := Classify(Signal{
		Kind:     "daemon_error",
		Symptom:  "event:error:window",
		Severity: "medium",
		Detail:   "ResizeObserver loop completed with undelivered notifications.",
	})
	if d.Action != ActionIgnore {
		t.Fatalf("action=%s want ignore: %s", d.Action, d.Reason)
	}
}

func TestT1194RealWindowErrorStillFilesPO(t *testing.T) {
	d := Classify(Signal{
		Kind:     "daemon_error",
		Symptom:  "event:error:window",
		Severity: "medium",
		Detail:   "ReferenceError: children is not defined",
	})
	if d.Action != ActionFilePO {
		t.Fatalf("action=%s want file+PO: %s", d.Action, d.Reason)
	}
}
