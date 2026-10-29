// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"testing"

	"github.com/marcelocantos/jevons/internal/tmpdirsweep"
)

func TestMain(m *testing.M) {
	tmpdirsweep.SweepPrefixes(os.TempDir(), []string{
		"TestT1036FailureHelper",
	}, nil)
	os.Exit(m.Run())
}
