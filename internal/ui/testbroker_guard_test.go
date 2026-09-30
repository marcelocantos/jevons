// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ui_test

import "github.com/marcelocantos/jevons/internal/testbroker"

// No test in this package reaches the owner's Claudia broker (🎯T975).
func init() { testbroker.DeadEnd() }
