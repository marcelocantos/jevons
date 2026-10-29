// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package testhygiene detects and sweeps residue test harnesses leave on the
// host (🎯T846): claudia-anchor tmux servers under journey isolate dirs, and
// fixture jevonsd processes rooted in go test temp trees.
package testhygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatload"
)

const journeySocketSuffix = "/claudia/tmux.sock"

// JourneyAnchor is a tmux server claudia started under a journey isolate root.
type JourneyAnchor struct {
	Root   string
	Socket string
	PID    int
}

var journeyAnchorRE = regexp.MustCompile(`tmux -S (\S+/jevons-journey-[^/]+` + regexp.QuoteMeta(journeySocketSuffix) + `) `)

// CensusJourneyAnchors reads the process table for claudia-anchor servers on
// isolate sockets.
func CensusJourneyAnchors() ([]JourneyAnchor, error) {
	tbl, err := seatload.HostTable()
	if err != nil {
		return nil, err
	}
	var out []JourneyAnchor
	for _, p := range tbl {
		m := journeyAnchorRE.FindStringSubmatch(p.Command)
		if m == nil {
			continue
		}
		sock := m[1]
		root := filepath.Dir(filepath.Dir(sock))
		out = append(out, JourneyAnchor{Root: root, Socket: sock, PID: p.PID})
	}
	return out, nil
}

// OrphanedJourneyAnchors returns anchors whose isolate is no longer running.
// The tmux server itself names the root; any other process with the root in
// argv is treated as a live isolate.
func OrphanedJourneyAnchors(anchors []JourneyAnchor, tbl seatload.Table) []JourneyAnchor {
	var orphans []JourneyAnchor
	for _, a := range anchors {
		if journeyAnchorOrphan(a, tbl) {
			orphans = append(orphans, a)
		}
	}
	return orphans
}

func journeyAnchorOrphan(a JourneyAnchor, tbl seatload.Table) bool {
	root := filepath.Clean(a.Root) + string(os.PathSeparator)
	for _, p := range tbl {
		if p.PID == a.PID {
			continue
		}
		if strings.Contains(p.Command, root) || strings.Contains(p.Command, a.Root) {
			return false
		}
	}
	return true
}

// KillJourneyAnchor stops the tmux server on the isolate socket.
func KillJourneyAnchor(sock string) {
	if _, err := os.Stat(sock); err != nil {
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return
	}
	_ = exec.Command("tmux", "-S", sock, "kill-server").Run()
}

// SweepOrphanedJourneyAnchors stops orphaned claudia-anchor servers and
// removes their isolate roots when nothing else references them. It returns
// how many anchors were reaped.
func SweepOrphanedJourneyAnchors() (int, error) {
	tbl, err := seatload.HostTable()
	if err != nil {
		return 0, err
	}
	anchors, err := CensusJourneyAnchors()
	if err != nil {
		return 0, err
	}
	orphans := OrphanedJourneyAnchors(anchors, tbl)
	for _, a := range orphans {
		KillJourneyAnchor(a.Socket)
		if journeyAnchorOrphan(a, tbl) {
			_ = os.RemoveAll(a.Root)
		}
	}
	return len(orphans), nil
}

// SweepStaleJourneyDirs removes jevons-journey-* directories under dir when no
// process names them and their claudia tmux socket is absent or already dead.
func SweepStaleJourneyDirs(dir string) (int, error) {
	tbl, err := seatload.HostTable()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var swept int
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "jevons-journey-") {
			continue
		}
		root := filepath.Join(dir, e.Name())
		if isolateRootLive(root, tbl) {
			continue
		}
		sock := filepath.Join(root, "claudia", "tmux.sock")
		KillJourneyAnchor(sock)
		if err := os.RemoveAll(root); err == nil {
			swept++
		}
	}
	return swept, nil
}

func isolateRootLive(root string, tbl seatload.Table) bool {
	marker := filepath.Clean(root)
	for _, p := range tbl {
		if strings.Contains(p.Command, marker) {
			return true
		}
	}
	return false
}
