// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package testhygiene

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/seatload"
)

func TestOrphanedJourneyAnchorsIgnoresLiveIsolate(t *testing.T) {
	root := "/var/folders/xx/T/jevons-journey-1234"
	sock := root + journeySocketSuffix
	anchor := JourneyAnchor{Root: root, Socket: sock, PID: 100}
	tbl := seatload.Table{
		{PID: 100, Command: "tmux -S " + sock + " new-session -d -s claudia-anchor"},
		{PID: 200, Command: root + "/jevonsd -port 55123 -workdir " + root},
	}
	if orphans := OrphanedJourneyAnchors([]JourneyAnchor{anchor}, tbl); len(orphans) != 0 {
		t.Fatalf("live isolate: got orphans %v", orphans)
	}
}

func TestOrphanedJourneyAnchorsFlagsTmuxAlone(t *testing.T) {
	root := "/var/folders/xx/T/jevons-journey-5678"
	sock := root + journeySocketSuffix
	anchor := JourneyAnchor{Root: root, Socket: sock, PID: 100}
	tbl := seatload.Table{
		{PID: 100, Command: "tmux -S " + sock + " new-session -d -s claudia-anchor"},
	}
	orphans := OrphanedJourneyAnchors([]JourneyAnchor{anchor}, tbl)
	if len(orphans) != 1 || orphans[0].PID != 100 {
		t.Fatalf("tmux-only isolate: got %v", orphans)
	}
}

func TestCensusFixtureDaemonsMatchesTestTempPath(t *testing.T) {
	tbl := seatload.Table{
		{PID: 1, Command: "/var/folders/xx/T/TestRestartThrashPolicy123/001/bin/jevonsd -port 60949"},
		{PID: 2, Command: "bin/jevonsd -port 13705 -workdir /Users/me/.jevons"},
	}
	got := CensusFixtureDaemons(tbl, map[int]string{1: "/var/folders/xx/T/TestRestartThrashPolicy123/001/bin/jevonsd", 2: "bin/jevonsd"})
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("fixture census = %v, want [1]", got)
	}
}

func TestT1207FixtureCensusDoesNotTreatArgumentMentionsAsExecutables(t *testing.T) {
	const fixture = "/var/folders/xx/T/TestRestartThrashPolicy123/001/bin/jevonsd"
	table := seatload.Table{
		{PID: 1, Command: fixture + " -port 60949"},
		{PID: 2, Command: "/bin/sh -c " + fixture + " -port 60949"},
		{PID: 3, Command: "git -C /var/folders/xx/T/TestRestartThrashPolicy123/001 show HEAD:cmd/jevonsd/main.go"},
		{PID: 4, Command: "rg jevonsd /var/folders/xx/T/TestRestartThrashPolicy123/001"},
		{PID: 5, Command: "/usr/bin/python3 inspect.py " + fixture},
		{PID: 6, Command: fixture + "-helper"},
		{PID: 7, Command: "/var/folders/xx/T/Testjevonsd123/001/bin/other"},
		{PID: 8, Command: "bin/jevonsd -port 13705"},
		{PID: 9, Command: ""},
		{PID: 10, Command: fixture},
	}
	executables, err := parsePSExecutablePaths("1 " + fixture + "\n2 /bin/sh\n3 /usr/bin/git\n4 /usr/bin/rg\n5 /usr/bin/python3\n6 " + fixture + "-helper\n7 /var/folders/xx/T/Testjevonsd123/001/bin/other\n8 bin/jevonsd\n10 " + fixture + "\n")
	if err != nil {
		t.Fatal(err)
	}
	got := CensusFixtureDaemons(table, executables)
	if len(got) != 2 || got[0] != 1 || got[1] != 10 {
		t.Fatalf("fixture executable census=%v, want [1 10]", got)
	}
}

func TestT1207FixtureExecutablePathPreservesSpaces(t *testing.T) {
	const path = "/var/folders/a space/T/TestRestart with spaces/001/bin/jevonsd"
	paths, err := parsePSExecutablePaths("  12    " + path + "\n")
	if err != nil || paths[12] != path {
		t.Fatalf("executable path lost spaces: %v %v", paths, err)
	}
	got := CensusFixtureDaemons(seatload.Table{{PID: 12, Command: path + " -port 1"}}, paths)
	if len(got) != 1 || got[0] != 12 {
		t.Fatalf("real fixture with spaces not detected: %v", got)
	}
}

func TestT1207MalformedExecutableSnapshotIsAnError(t *testing.T) {
	for _, output := range []string{"broken", "x /bin/sh", "12   ", "0 /bin/sh"} {
		if _, err := parsePSExecutablePaths(output); err == nil {
			t.Fatalf("accepted malformed process metadata: %q", output)
		}
	}
}
