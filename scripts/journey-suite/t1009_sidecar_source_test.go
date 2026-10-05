// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia/omp"
)

// 🎯T1009: the journey isolate stages its Claudia sidecar from the pinned
// module. When that broke, the journey suite could not start at all, and
// v0.14.0 and v0.15.0 shipped without a single journey having run: the
// release path runs go test ./..., not the journeys, which need live
// providers. This test runs in that go test, so a pin or a stand-in that
// takes the sidecar source away fails the release build instead.
func TestJourneyIsolateFindsTheSidecarInThePinnedModule(t *testing.T) {
	t.Setenv("CLAUDIA_OMP_SERVER", "")
	script := omp.ServerScript()
	if strings.TrimSpace(script) == "" {
		t.Fatal("the pinned claudia module names no sidecar server script")
	}
	dir := filepath.Dir(script)
	for _, name := range []string{"server.ts", "package.json", "bun.lockb"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("journey isolate cannot stage the sidecar: %s missing from %s: %v", name, dir, err)
		}
	}
}
