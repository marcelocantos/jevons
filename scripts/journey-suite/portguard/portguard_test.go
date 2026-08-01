// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portguard

import (
	"strings"
	"testing"
)

func TestRefuseDaily(t *testing.T) {
	if DailyPort != 13705 {
		t.Fatalf("DailyPort = %d, want 13705", DailyPort)
	}
	if DefaultPort == DailyPort {
		t.Fatal("DefaultPort must not equal DailyPort")
	}

	err := RefuseDaily(DailyPort)
	if err == nil {
		t.Fatal("RefuseDaily(DailyPort) = nil, want error")
	}
	msg := err.Error()
	for _, want := range []string{"refusing port", "daily-driver", "13705"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}

	for _, p := range []int{DefaultPort, 0, 13716} {
		if err := RefuseDaily(p); err != nil {
			t.Errorf("RefuseDaily(%d) = %v, want nil", p, err)
		}
	}
}
