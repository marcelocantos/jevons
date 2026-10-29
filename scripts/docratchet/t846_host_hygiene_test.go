// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/seatload"
	"github.com/marcelocantos/jevons/internal/testhygiene"
)

// 🎯T846: finished isolates and fixture tests must not leave claudia-anchor
// tmux servers or throwaway jevonsd on the host. Daemon-starting teardown is
// pinned by TestT1036FailedRestartFixtureLeavesNoDaemon in this package.
func TestT846HostHasNoOrphanedJourneyAnchorsOrFixtureDaemons(t *testing.T) {
	sweptAnchors, err := testhygiene.SweepOrphanedJourneyAnchors()
	if err != nil {
		t.Fatal(err)
	}
	tmp := os.TempDir()
	if td := os.Getenv("TMPDIR"); td != "" {
		tmp = td
	}
	sweptDirs, err := testhygiene.SweepStaleJourneyDirs(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if sweptAnchors > 0 || sweptDirs > 0 {
		t.Logf("one-time sweep removed %d orphaned anchor(s) and %d stale journey dir(s)", sweptAnchors, sweptDirs)
	}

	tbl, err := seatload.HostTable()
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := testhygiene.CensusJourneyAnchors()
	if err != nil {
		t.Fatal(err)
	}
	orphans := testhygiene.OrphanedJourneyAnchors(anchors, tbl)
	if len(orphans) > 0 {
		var lines []string
		for _, o := range orphans {
			lines = append(lines, fmt.Sprintf("pid=%d root=%s", o.PID, o.Root))
		}
		t.Fatalf("orphaned journey tmux anchor(s): %s", strings.Join(lines, "; "))
	}
	leaked, err := testhygiene.CensusLeakedFixtureDaemons()
	if err != nil {
		t.Fatal(err)
	}
	if len(leaked) > 0 {
		t.Fatalf("fixture jevonsd still running from a go test temp dir: %v", leaked)
	}
}
