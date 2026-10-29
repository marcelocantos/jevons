// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"testing"

	"github.com/marcelocantos/jevons/internal/tmpdirsweep"
)

func TestMain(m *testing.M) {
	tmpdirsweep.SweepPrefixes(os.TempDir(), []string{
		"TestForegroundRestartSurvivesItsCallerBeingKilled",
		"TestForegroundRestartDiesWithoutSelfDetach",
	}, nil)
	os.Exit(m.Run())
}
