// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package cost

import "testing"

func TestT1043DefaultSessionBound(t *testing.T) {
	if got := DefaultBudgetConfig().MaxSessions; got != 100 {
		t.Fatalf("default max sessions = %d, want 100", got)
	}
}
