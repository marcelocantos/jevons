// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktreereap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config is one sweep of one clone.
type Config struct {
	Policy
	// DryRun decides everything and removes nothing. The report is
	// identical either way, which is what makes it worth reading before
	// letting the sweeper loose on a clone for the first time.
	DryRun bool
	// Log receives progress lines. nil discards them.
	Log func(string)
}

func (c Config) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(fmt.Sprintf(format, args...))
	}
}

// Verdict is what the sweep decided about one worktree, and what came of it.
type Verdict struct {
	Entry    Entry
	Owner    Owner
	Idle     time.Duration
	Decision Decision
	Removed  bool
	Err      error // removal failed; the entry is still there
}

// Result is the whole sweep, in the order git listed the worktrees.
type Result struct {
	Verdicts []Verdict
	Removed  int
	Held     int
	Failed   int
}

// Report renders the sweep for a log or a chat line: every entry, its verdict,
// and the reason. Held and failed entries are the point of it — a reaper that
// only reports what it deleted cannot be audited for what it should have.
func (r Result) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "worktree sweep: %d entries, %d removed, %d held, %d failed\n", len(r.Verdicts), r.Removed, r.Held, r.Failed)
	for _, v := range r.Verdicts {
		verb := v.Decision.Action.String()
		switch {
		case v.Err != nil:
			verb = "FAILED to remove"
		case v.Decision.Action == ActionRemove && !v.Removed:
			verb = "would remove"
		case v.Removed:
			verb = "removed"
		}
		fmt.Fprintf(&b, "  %-16s %s — %s", verb, v.Entry.Path, v.Decision.Reason)
		if v.Owner.Kind != OwnerUnknown {
			fmt.Fprintf(&b, " [owner: %s]", v.Owner.Kind)
		}
		if v.Err != nil {
			fmt.Fprintf(&b, ": %v", v.Err)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Sweep lists the clone's worktrees, decides each one, and removes those whose
// owner is gone.
//
// It is a function rather than a method on a long-lived object because it holds
// nothing between runs: the process table, the marker files and git's own list
// are re-read every time. Two sweeps racing each other is therefore harmless —
// the loser finds the worktree already gone, which the policy reads as "the
// directory no longer exists" and git reports as an ordinary removal error on a
// path that is already absent.
func Sweep(cfg Config) (Result, error) {
	var res Result

	cfg.Policy = cfg.Policy.WithDefaults()
	root := cfg.RepoRoot
	if root == "" {
		return res, fmt.Errorf("worktreereap: no repo root")
	}

	entries, err := listWorktrees(root)
	if err != nil {
		return res, err
	}

	commonDir, err := git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return res, fmt.Errorf("worktreereap: locate git dir of %s: %w", root, err)
	}
	markers := readMarkers(commonDir)

	live, err := Snapshot()
	if err != nil {
		// Without a process table every live worktree looks abandoned.
		// Refusing to sweep is the only safe reading of that.
		return res, fmt.Errorf("worktreereap: read process table: %w", err)
	}

	// The sweeping process's own worktree is protected whatever its owner
	// looks like. A caller running inside a verification worktree — a test,
	// a build, a shell — would otherwise be able to delete the ground it is
	// standing on, and no report afterwards would be readable.
	protected := append([]string(nil), cfg.Protected...)
	if wd, err := os.Getwd(); err == nil {
		protected = append(protected, wd)
	}
	cfg.Protected = protected

	for _, e := range entries {
		obs := observe(e, markers)
		v := Verdict{Entry: e, Owner: obs.Owner, Idle: obs.Idle}
		v.Decision = Decide(obs, live, cfg.Policy)

		switch v.Decision.Action {
		case ActionRemove:
			if !cfg.DryRun {
				if err := remove(root, e.Path); err != nil {
					v.Err = err
					res.Failed++
				} else {
					v.Removed = true
					res.Removed++
				}
			}
		case ActionHold:
			res.Held++
		}
		res.Verdicts = append(res.Verdicts, v)
	}

	if res.Removed > 0 {
		// Drops administrative entries for directories that are now gone.
		if _, err := git(root, "worktree", "prune"); err != nil {
			cfg.logf("worktree prune after sweep: %v", err)
		}
	}
	if res.Removed > 0 || res.Failed > 0 || res.Held > 0 {
		cfg.logf("%s", strings.TrimRight(res.Report(), "\n"))
	}
	return res, nil
}

// observe gathers everything the policy needs about one worktree. Every field
// it cannot establish is left in its "unknown" state rather than guessed at,
// because the policy is written to hold rather than remove on unknowns.
func observe(e Entry, markers markerIndex) Observation {
	obs := Observation{Entry: e, Idle: -1}

	if fi, err := os.Stat(e.Path); err != nil || !fi.IsDir() {
		obs.Exists = false
		return obs
	}
	obs.Exists = true

	if m, ok := markers[filepath.Clean(e.Path)]; ok {
		obs.Owner = Owner{Kind: OwnerMarker, PID: m.PID, Cmd: m.Cmd, Session: m.Session}
	} else if s := SessionFromPath(e.Path); s != "" {
		obs.Owner = Owner{Kind: OwnerSession, Session: s}
	}

	if t, ok := newestWrite(e.Path); ok {
		if idle := time.Since(t); idle >= 0 {
			obs.Idle = idle
		} else {
			obs.Idle = 0
		}
	}

	// --untracked-files=no on purpose: a verification worktree is expected
	// to be full of build output, and treating that as "somebody's work"
	// would hold every entry the sweeper exists to remove.
	if out, err := git(e.Path, "status", "--porcelain", "--untracked-files=no"); err == nil {
		obs.DirtyTracked = strings.TrimSpace(out) != ""
	}

	return obs
}

// walkDepth bounds how far into a worktree newestWrite descends, and walkCap
// how many directories it will visit. A verification worktree holds a full
// build tree and a node_modules; reading every inode in it once a minute
// would cost more than the disk the sweep reclaims. Depth 3 sees the build
// outputs that matter (bin/, web/node_modules, internal/*/) and the cap keeps
// a pathological tree from stalling the loop.
const (
	walkDepth = 3
	walkCap   = 4096
)

// newestWrite returns the most recent modification time under dir, and whether
// anything could be read at all.
//
// Directory mtimes are included as well as file mtimes: a build that adds
// files deep inside a subtree touches that subtree's directory even when the
// walk stops short of the files themselves, so the coarse answer still moves
// when the tree is in use. The bias is towards reporting a tree as *more*
// recently written than it was, which can only hold a worktree the sweeper
// might have removed.
func newestWrite(dir string) (time.Time, bool) {
	var newest time.Time
	visited := 0

	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		if visited >= walkCap {
			return
		}
		visited++
		ents, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, ent := range ents {
			// .git in a linked worktree is a file pointing at the admin
			// directory, and git rewrites it on operations we do not want
			// to read as the owner still working.
			if ent.Name() == ".git" {
				continue
			}
			info, err := ent.Info()
			if err != nil {
				continue
			}
			if t := info.ModTime(); t.After(newest) {
				newest = t
			}
			if ent.IsDir() && depth < walkDepth {
				walk(filepath.Join(path, ent.Name()), depth+1)
			}
		}
	}

	if fi, err := os.Stat(dir); err == nil {
		newest = fi.ModTime()
	}
	walk(dir, 1)
	return newest, !newest.IsZero()
}

// remove deletes one worktree, directory and administrative entry alike.
//
// --force is required and is the whole reason this package is careful about
// who owns what: a verification worktree always has untracked build output, so
// git refuses the polite form every single time. The fallback exists because
// `git worktree remove` also refuses a worktree whose admin data git considers
// broken — exactly the state a killed creator can leave behind — and leaving
// the 289MB on disk because the metadata is damaged would defeat the sweep.
func remove(root, path string) error {
	if out, err := exec.Command("git", "-C", root, "worktree", "remove", "--force", path).CombinedOutput(); err != nil {
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return fmt.Errorf("git worktree remove: %v: %s; rm: %w", err, strings.TrimSpace(string(out)), rmErr)
		}
		if _, pruneErr := git(root, "worktree", "prune"); pruneErr != nil {
			return fmt.Errorf("removed %s but prune failed: %w", path, pruneErr)
		}
	}
	return nil
}

// listWorktrees parses `git worktree list --porcelain`. The first record git
// prints is always the clone itself, which is how Main is identified.
func listWorktrees(root string) ([]Entry, error) {
	out, err := git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("worktreereap: list worktrees of %s: %w", root, err)
	}

	var entries []Entry
	var cur *Entry
	flush := func() {
		if cur != nil {
			cur.Main = len(entries) == 0
			entries = append(entries, *cur)
			cur = nil
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			flush()
			cur = &Entry{Path: filepath.Clean(val)}
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = val
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
			}
		case "bare":
			if cur != nil {
				cur.Bare = true
			}
		}
	}
	flush()
	return entries, nil
}

// Snapshot reads the process table once, so that every worktree in a sweep is
// judged against the same instant. A worktree reaped because its owner died
// between two `ps` calls would be indistinguishable from a bug.
func Snapshot() (Liveness, error) {
	out, err := exec.Command("ps", "-eo", "pid=,command=").Output()
	if err != nil {
		return Liveness{}, err
	}
	live := Liveness{Cmds: map[int]string{}}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimLeft(line, " ")
		pidStr, cmd, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		live.Cmds[pid] = strings.TrimSpace(cmd)
	}
	if len(live.Cmds) == 0 {
		return live, fmt.Errorf("ps listed no processes")
	}
	return live, nil
}

// git runs a git command and returns trimmed stdout, folding stderr into the
// error because a git failure here is always something an operator must read.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// Paths returns the worktree paths a result saw, sorted, for tests and for
// callers that want the list without the prose.
func (r Result) Paths() []string {
	paths := make([]string, 0, len(r.Verdicts))
	for _, v := range r.Verdicts {
		paths = append(paths, v.Entry.Path)
	}
	sort.Strings(paths)
	return paths
}
