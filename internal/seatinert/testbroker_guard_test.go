// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatinert

import "github.com/marcelocantos/jevons/internal/testbroker"

// Tests never reach the owner's Claudia broker (🎯T975).
func init() { testbroker.DeadEnd() }
