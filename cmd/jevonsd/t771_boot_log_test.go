// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

// 🎯T771: boot markers must be emitted where supervisord captures them — early
// listen, converge loop start, and stderr fsync under SUPERVISOR_ENABLED.
func TestT771BootLogMarkers(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, needle := range []string{
		`"jevonsd listening"`,
		`superviselog.SlogWriter(os.Stderr)`,
		`srv.StartCockpitConverge(ctx, server.DefaultCockpitInterval)`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("main.go missing 🎯T771 marker %q", needle)
		}
	}
	listen := strings.Index(body, `slog.Info("jevonsd listening"`)
	converge := strings.Index(body, `srv.StartCockpitConverge(ctx, server.DefaultCockpitInterval)`)
	if listen < 0 || converge < 0 || listen > converge {
		t.Fatalf("listen log must precede converge loop: listen=%d converge=%d", listen, converge)
	}
}
