// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktreereap

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// markerName is the file, inside git's own administrative directory for a
// linked worktree, that records who created it.
//
// It lives there rather than inside the worktree on purpose. A file in the
// worktree would show up as untracked in `git status`, would be seen by any
// build or test running in there, and would have to be excluded from every
// clean-checkout ratchet that asserts the tree matches HEAD. The admin
// directory has none of those problems, and git already deletes it for us when
// the worktree is properly removed or pruned — so a marker cannot outlive the
// worktree it describes.
const markerName = "jevons-owner"

// Marker records the process that created a verification worktree.
//
// Cmd is the creating process's command line at the moment of creation, and it
// is the field that makes the pid trustworthy: pids are recycled, so "pid 4711
// exists" proves nothing on its own, while "pid 4711 exists and is still
// running the command that took this worktree out" does.
type Marker struct {
	PID     int       `json:"pid"`
	Cmd     string    `json:"cmd"`
	Session string    `json:"session,omitempty"`
	Created time.Time `json:"created"`
	Note    string    `json:"note,omitempty"`
}

// MarkArgs asks for a worktree to be stamped with its owner.
type MarkArgs struct {
	// Worktree is the worktree directory to stamp.
	Worktree string
	// PID is the process that owns it. Zero means the calling process.
	PID int
	// Session is an optional session uuid. Empty means: take it from the
	// worktree path if it carries one.
	Session string
	// Note is free text shown in sweep reports, e.g. the test name.
	Note string
}

// Mark records the owner of an existing worktree.
//
// Call it immediately after `git worktree add`, from the process that will be
// using the worktree. That is the whole contract, and it is deliberately the
// only thing a caller has to remember — everything else about the lifetime is
// decided from outside by Sweep, precisely because the caller cannot be relied
// on to still be alive at the end (🎯T440).
func Mark(args *MarkArgs) error {
	if args == nil || args.Worktree == "" {
		return fmt.Errorf("worktreereap: Mark needs a worktree path")
	}
	pid := args.PID
	if pid == 0 {
		pid = os.Getpid()
	}
	admin, err := adminDir(args.Worktree)
	if err != nil {
		return err
	}
	session := args.Session
	if session == "" {
		session = SessionFromPath(args.Worktree)
	}
	m := Marker{
		PID:     pid,
		Cmd:     commandLine(pid),
		Session: session,
		Created: time.Now(),
		Note:    args.Note,
	}
	blob, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("worktreereap: encode marker: %w", err)
	}
	return writeAtomic(filepath.Join(admin, markerName), blob)
}

// adminDir returns git's administrative directory for the given worktree,
// which is where the marker is kept.
func adminDir(worktree string) (string, error) {
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return "", fmt.Errorf("worktreereap: %s is not a git worktree: %w", worktree, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("worktreereap: git reported no admin dir for %s", worktree)
	}
	return dir, nil
}

// writeAtomic writes through a temp file in the same directory and renames, so
// a sweep reading concurrently never sees a half-written marker. A marker that
// cannot be parsed is treated as absent, which would silently downgrade a
// worktree to the weaker session signal — worth one rename to avoid.
func writeAtomic(path string, blob []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jevons-owner-*")
	if err != nil {
		return fmt.Errorf("worktreereap: create marker temp: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return fmt.Errorf("worktreereap: write marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("worktreereap: close marker: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("worktreereap: install marker: %w", err)
	}
	return nil
}

// markerIndex maps a worktree path to the marker found in its admin
// directory. It is built once per sweep from a single directory scan rather
// than by running git once per worktree.
type markerIndex map[string]Marker

// readMarkers scans <commonGitDir>/worktrees/*/ for markers. Each admin
// directory holds a `gitdir` file naming the worktree it belongs to, which is
// how an admin directory is matched back to a path without asking git.
//
// Errors reading any individual entry are skipped rather than propagated: a
// missing or malformed marker means "owner unknown", which the policy already
// handles conservatively, and one unreadable entry must not blind the sweep to
// the other thirteen.
func readMarkers(commonGitDir string) markerIndex {
	idx := markerIndex{}
	root := filepath.Join(commonGitDir, "worktrees")
	entries, err := os.ReadDir(root)
	if err != nil {
		return idx
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		admin := filepath.Join(root, e.Name())
		gitdir, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		// `gitdir` holds "<worktree>/.git"; the worktree is its parent.
		wt := filepath.Clean(filepath.Dir(strings.TrimSpace(string(gitdir))))
		blob, err := os.ReadFile(filepath.Join(admin, markerName))
		if err != nil {
			continue
		}
		var m Marker
		if err := json.Unmarshal(blob, &m); err != nil {
			continue
		}
		idx[wt] = m
	}
	return idx
}

// commandLine reads a process's current command line, or "" if it cannot be
// read. An empty recorded command makes PIDAlive fall back to pid existence
// alone, which is weaker but never wrong in the unsafe direction: it can only
// keep a worktree that could have been reaped.
func commandLine(pid int) string {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
