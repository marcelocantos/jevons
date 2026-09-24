// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsDailyStateDir(t *testing.T) {
	daily := Default().StateDir
	cases := []struct {
		dir  string
		want bool
	}{
		{daily, true},
		{daily + "/", true},
		{filepath.Join(daily, "journey"), false},
		{daily + "-journey", false},
		{t.TempDir(), false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsDailyStateDir(c.dir); got != c.want {
			t.Errorf("IsDailyStateDir(%q) = %v, want %v", c.dir, got, c.want)
		}
	}
}

func TestRefuseJourneyDevState(t *testing.T) {
	daily := Default().StateDir
	isolate := t.TempDir()

	if err := RefuseJourneyDevState(JourneyPort, daily); err == nil {
		t.Fatal("JourneyPort + daily state_dir must refuse")
	} else {
		msg := err.Error()
		for _, want := range []string{"refusing port", "13715", "state_dir", "throwaway"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q missing %q", msg, want)
			}
		}
	}

	if err := RefuseJourneyDevState(JourneyPort, isolate); err != nil {
		t.Fatalf("JourneyPort + isolate state_dir must allow: %v", err)
	}
	if err := RefuseJourneyDevState(DevelopmentPort, daily); err != nil {
		t.Fatalf("DevelopmentPort + daily state_dir must allow: %v", err)
	}
	if err := RefuseJourneyDevState(13716, daily); err != nil {
		t.Fatalf("other port + daily state_dir must allow: %v", err)
	}
	if JourneyPort == DevelopmentPort {
		t.Fatal("JourneyPort must not equal DevelopmentPort")
	}
	if VanillaPort == DevelopmentPort || VanillaPort == JourneyPort {
		t.Fatal("VanillaPort must be distinct from DevelopmentPort and JourneyPort")
	}
}

func TestRefuseVanillaPortAsPrimary(t *testing.T) {
	if err := RefuseVanillaPortAsPrimary(VanillaPort); err == nil {
		t.Fatal("VanillaPort as primary must refuse")
	} else {
		msg := err.Error()
		for _, want := range []string{"13706", "sidecar", "13705"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q missing %q", msg, want)
			}
		}
	}
	if err := RefuseVanillaPortAsPrimary(DevelopmentPort); err != nil {
		t.Fatalf("DevelopmentPort as primary must allow: %v", err)
	}
	if err := RefuseVanillaPortAsPrimary(JourneyPort); err != nil {
		t.Fatalf("JourneyPort as primary must allow: %v", err)
	}
}
