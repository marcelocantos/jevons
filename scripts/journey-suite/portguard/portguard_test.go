// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portguard

import (
	"strings"
	"testing"
)

func TestRefuseDevelopment(t *testing.T) {
	if DevelopmentPort != 13705 {
		t.Fatalf("DevelopmentPort = %d, want 13705", DevelopmentPort)
	}
	if DefaultPort == DevelopmentPort {
		t.Fatal("DefaultPort must not equal DevelopmentPort")
	}

	err := RefuseDevelopment(DevelopmentPort)
	if err == nil {
		t.Fatal("RefuseDevelopment(DevelopmentPort) = nil, want error")
	}
	msg := err.Error()
	for _, want := range []string{"refusing port", "development", "13705"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}

	if err := RefuseDevelopment(13706); err == nil {
		t.Fatal("RefuseDevelopment(13706) = nil, want error (vanilla sidecar)")
	} else if !strings.Contains(err.Error(), "sidecar") {
		t.Errorf("13706 error %q missing sidecar", err)
	}

	for _, p := range []int{DefaultPort, 0, 13716} {
		if err := RefuseDevelopment(p); err != nil {
			t.Errorf("RefuseDevelopment(%d) = %v, want nil", p, err)
		}
	}
}
