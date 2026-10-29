// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"strings"
	"testing"
)

func t1162LandingPackages(repo, commit string) ([]string, error) {
	_ = repo
	_ = commit
	return []string{"internal/mcpserver", "internal/fleetpanel"}, nil
}

func t1162GreenRecord(id, commit string, command []string) *Record {
	return &Record{
		ID: id, Name: "t1162", StatusKnown: true, ExitStatus: 0, Verdict: VerdictGreen,
		Command: command,
		Tree:    &TreeProvenance{Commit: commit, Clean: true},
	}
}

func t1162Check(attestation string, rec *Record) AchieveResult {
	return CheckAchieve(&AchieveCheckArgs{
		Attestation: attestation,
		Lookup: func(id string) (*Record, bool) {
			if rec != nil && rec.ID == id {
				return rec, true
			}
			return nil, false
		},
		Contains:          func(code, fix string) bool { return sameCommit(code, fix) },
		IsCommit:          func(sha string) bool { return len(sha) >= 7 },
		Repo:              "/repo",
		ChangedGoPackages: t1162LandingPackages,
	})
}

// 🎯T1162 hermetic: landing touched mcpserver and fleetpanel; -run on one package is refused.
func TestT1162AchieveRefusesNarrowRunAndMissingPackages(t *testing.T) {
	fix := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rec := t1162GreenRecord("c1162gate", fix, []string{"go", "test", "-run", "T1121", "./internal/mcpserver"})
	att := "Landing " + fix + ". GATE t1162 exit=0 GREEN id=c1162gate"
	r := t1162Check(att, rec)
	if r.Verdict != AchieveRefused {
		t.Fatalf("verdict=%s want refused: %s", r.Verdict, r.Detailed())
	}
	if !t765HasFlag(r, FlagAchieveGateNarrowRun) {
		t.Fatalf("want narrow run flag, got %v", r.Flags)
	}
	if !t765HasFlag(r, FlagAchieveGateMissingPackages) {
		t.Fatalf("want missing packages flag, got %v", r.Flags)
	}
	if !strings.Contains(r.Detailed(), "internal/fleetpanel") {
		t.Fatalf("missing packages detail should name fleetpanel: %s", r.Detailed())
	}
}

func TestT1162AchieveAcceptsFullPackageGate(t *testing.T) {
	fix := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rec := t1162GreenRecord("fullgate", fix, []string{
		"go", "test", "-count=1", "./internal/mcpserver/...", "./internal/fleetpanel/...",
	})
	att := fix + " GATE full exit=0 GREEN id=fullgate"
	r := t1162Check(att, rec)
	if r.Verdict != AchieveVerified {
		t.Fatalf("verdict=%s want verified: %s", r.Verdict, r.Detailed())
	}
}

func TestT1162AchieveAcceptsMakeTestGo(t *testing.T) {
	fix := "cccccccccccccccccccccccccccccccccccccccc"
	rec := t1162GreenRecord("maketest", fix, []string{"make", "test-go"})
	att := fix + " GATE make-test-go exit=0 GREEN id=maketest"
	r := t1162Check(att, rec)
	if r.Verdict != AchieveVerified {
		t.Fatalf("verdict=%s want verified: %s", r.Verdict, r.Detailed())
	}
}

func TestPatternCoversPackage(t *testing.T) {
	cases := []struct {
		pattern, pkg string
		want         bool
	}{
		{"./...", "internal/mcpserver", true},
		{"internal/mcpserver/...", "internal/mcpserver", true},
		{"internal/mcpserver", "internal/mcpserver", true},
		{"internal/mcpserver", "internal/fleetpanel", false},
	}
	for _, tc := range cases {
		if got := patternCoversPackage(tc.pattern, tc.pkg); got != tc.want {
			t.Fatalf("patternCoversPackage(%q,%q)=%v want %v", tc.pattern, tc.pkg, got, tc.want)
		}
	}
}
