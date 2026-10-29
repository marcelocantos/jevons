// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT794RestartScriptReportsPreShutdownAndTiming(t *testing.T) {
	body := readRepo(t, "scripts/restart-jevonsd.sh")
	for _, needle := range []string{
		"🎯T794 pre-shutdown",
		"pre_shutdown_build=",
		"BOUNCE_WORK_START",
		"BOUNCE_SHUTDOWN_EPOCH",
		"cmd/bouncetiming",
		"report_t794_bounce_timing",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("restart-jevonsd.sh missing 🎯T794 marker %q", needle)
		}
	}
}

func TestT794MainStartsConvergeBeforeImpatience(t *testing.T) {
	body := readRepo(t, "cmd/jevonsd/main.go")
	converge := strings.Index(body, "srv.StartCockpitConverge(ctx, server.DefaultCockpitInterval)")
	sticky := strings.Index(body, "sticky.NewOSNotifier")
	sec := strings.Index(body, "standing security auditor")
	if converge < 0 || sticky < 0 || sec < 0 {
		t.Fatalf("missing markers converge=%d sticky=%d sec=%d", converge, sticky, sec)
	}
	if !(converge < sticky && converge < sec) {
		t.Fatalf("converge must start before impatience/security: converge=%d sticky=%d sec=%d", converge, sticky, sec)
	}
}
