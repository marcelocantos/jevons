// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

// 🎯T40 oracle: upgrade mode must not stop agents.
func TestModeUpgradeDoesNotStopAgents(t *testing.T) {
	if ModeUpgrade.StopAgents() {
		t.Fatal("ModeUpgrade.StopAgents() = true; upgrade exit must skip StopAll")
	}
	if !ModeNormal.StopAgents() {
		t.Fatal("ModeNormal.StopAgents() = false; normal exit must StopAll")
	}
}

func TestModeString(t *testing.T) {
	if ModeUpgrade.String() != "upgrade" {
		t.Fatalf("upgrade string = %q", ModeUpgrade.String())
	}
	if ModeNormal.String() != "normal" {
		t.Fatalf("normal string = %q", ModeNormal.String())
	}
}

func TestEnvRequestsUpgrade(t *testing.T) {
	t.Setenv(EnvUpgradeExit, "")
	if EnvRequestsUpgrade() {
		t.Fatal("empty env should not request upgrade")
	}
	t.Setenv(EnvUpgradeExit, "1")
	if !EnvRequestsUpgrade() {
		t.Fatal("JEVONS_UPGRADE_EXIT=1 should request upgrade")
	}
	t.Setenv(EnvUpgradeExit, "true")
	if !EnvRequestsUpgrade() {
		t.Fatal("true should request upgrade")
	}
	t.Setenv(EnvUpgradeExit, "0")
	if EnvRequestsUpgrade() {
		t.Fatal("0 should not request upgrade")
	}
}

func TestSnapshotRoundTripPreservesSessionIDs(t *testing.T) {
	dir := t.TempDir()
	path := SnapshotPath(dir)
	snap := BuildSnapshot([]Handle{
		{Name: "jevons", SessionID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", WorkDir: "/tmp/o", Alive: true},
		{Name: "worker", SessionID: "11111111-2222-3333-4444-555555555555", Alive: false},
	}, 4242)
	if snap.Residual == "" {
		t.Fatal("snapshot must document residual")
	}
	if err := SaveSnapshot(path, snap); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected snapshot")
	}
	if got.CoordinatorPID != 4242 {
		t.Fatalf("coordinator pid = %d", got.CoordinatorPID)
	}
	ids := SessionIDs(got)
	if len(ids) != 2 {
		t.Fatalf("session ids = %v", ids)
	}
	if ids[0] != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("first session = %q", ids[0])
	}
}

func TestLoadSnapshotMissingIsNil(t *testing.T) {
	got, err := LoadSnapshot(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || got != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", got, err)
	}
}

func TestLoadSnapshotMalformedIsHardError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSnapshot(path)
	if err == nil {
		t.Fatal("expected hard error on malformed snapshot")
	}
}

// ShutdownPlan mirrors the coordinator exit decision so tests pin the
// contract without spinning jevonsd.
func TestShutdownPlanSkipsStopAllOnUpgrade(t *testing.T) {
	type plan struct {
		mode Mode
	}
	decide := func(sighup, envUpgrade bool) plan {
		if sighup || envUpgrade {
			return plan{mode: ModeUpgrade}
		}
		return plan{mode: ModeNormal}
	}

	cases := []struct {
		name       string
		sighup     bool
		env        bool
		wantStop   bool
		wantUpgrade bool
	}{
		{"sigterm_default", false, false, true, false},
		{"sighup", true, false, false, true},
		{"env_on_sigterm", false, true, false, true},
		{"both", true, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := decide(tc.sighup, tc.env)
			if p.mode.StopAgents() != tc.wantStop {
				t.Fatalf("StopAgents = %v, want %v", p.mode.StopAgents(), tc.wantStop)
			}
			if (p.mode == ModeUpgrade) != tc.wantUpgrade {
				t.Fatalf("mode = %v, want upgrade=%v", p.mode, tc.wantUpgrade)
			}
		})
	}
}
