// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package worktreereap keeps `git worktree list` in the shared clone a
// readable answer by removing verification worktrees whose owner is gone
// (🎯T440).
//
// THE FAULT. Every rail that matters in this repo tells a worker to do its
// checking in a detached worktree — 🎯T398 (run the web suite on the committed
// tree), 🎯T376 and 🎯T377 (the shared clone's tree and index are not yours),
// 🎯T254.2 (build the daemon from a snapshot, not from everyone's typing). So
// `git worktree list` is a load-bearing surface: it is where a worker looks to
// find out what is checked out where. On 2026-08-15 it had seventeen entries,
// of which fourteen belonged to sessions that were over and three to the
// docratchet ratchet itself, holding 289MB and pinning stale commits.
//
// WHY THE CLEANUP THAT EXISTS DOES NOT WORK. Both ratchet tests do defer a
// `worktree remove --force` plus a `prune`, which is why the leak is
// interesting rather than sloppy. Cleanup that lives inside the dying process
// does not survive the way that process dies: a timeout panic, a SIGKILL, a
// harness that drops the session — none of them run `t.Cleanup`, and none of
// them run `defer`. This is the same finding as 🎯T405, where a restart script
// documented "invoke me detached" as an instruction to its callers: a
// correctness property that depends on the dying party remembering something
// is not a property. It has to be decided from outside, by something still
// alive.
//
// WHY `git worktree prune` IS NOT THE ANSWER. prune reaps the administrative
// entry of a worktree whose *directory is missing*. Every leaked entry here
// has its directory sitting right there on disk, fully populated — that is
// what the 289MB is. To prune, the directories would have to be deleted first,
// which is the entire question, not the answer to it.
//
// HOW OWNERSHIP IS DECIDED. Three signals, strongest first, because they
// differ in what they actually prove:
//
//   - A MARKER (Mark, written into git's own admin directory for that
//     worktree) names the creating process by pid and by the command line it
//     had at creation. A pid that is gone, or reused by an unrelated command,
//     is definitive: that process will not come back, so its worktree is
//     reaped on the next cycle with no waiting period.
//   - A SESSION UUID in the path — the harness puts each session's scratchpad
//     under .../<uuid>/scratchpad/… — is checked against live process command
//     lines. This proves less than a pid: jevonsd deliberately stops idle
//     agent processes and rehydrates them later, so a session with no live
//     process may still be a worker that is merely parked. Those wait out
//     SessionGrace before they are touched.
//   - NOTHING identifiable falls back to how long the tree has been idle, and
//     waits out the longer IdleGrace, because a guess deserves more rope than
//     a fact.
//
// WHAT IS NEVER REMOVED. The clone itself, any path the caller declares
// protected (the daemon's build snapshot), a locked worktree, a worktree
// checked out on a branch rather than detached (that is somebody's work, not a
// verification tree), and any worktree with modified tracked files. Those last
// three are reported rather than silently retained, so an entry that survives
// a sweep still says why — a reaper whose refusals are invisible is a reaper
// nobody can trust with --force.
package worktreereap

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Entry is one row of `git worktree list --porcelain`.
type Entry struct {
	Path     string // absolute path of the worktree
	Head     string // commit the worktree is checked out at
	Branch   string // full ref name, empty when detached
	Detached bool
	Locked   bool
	Bare     bool
	Main     bool // the clone itself, always the first row git prints
}

// OwnerKind says which signal identified a worktree's creator, and therefore
// how much the identification is worth.
type OwnerKind int

const (
	// OwnerUnknown: nothing identifies the creator. Only idle time is left.
	OwnerUnknown OwnerKind = iota
	// OwnerSession: a session uuid in the path. A session with no live
	// process may still be a parked worker, so this is not proof of death.
	OwnerSession
	// OwnerMarker: a marker names the creating process. A dead pid is proof.
	OwnerMarker
)

func (k OwnerKind) String() string {
	switch k {
	case OwnerMarker:
		return "marker"
	case OwnerSession:
		return "session"
	default:
		return "unknown"
	}
}

// Owner is what could be established about who created a worktree.
type Owner struct {
	Kind    OwnerKind
	PID     int    // marker only
	Cmd     string // command line recorded at creation, marker only
	Session string // session uuid, from the marker or from the path
}

// Observation is everything Decide needs to know about one worktree. It is
// gathered by Sweep and passed to the pure policy so the policy itself never
// touches the filesystem, the process table, or git.
type Observation struct {
	Entry Entry
	Owner Owner

	// Exists is false when git still lists the worktree but its directory
	// is gone — the one case plain `git worktree prune` already handles.
	Exists bool

	// Idle is how long since anything under the worktree was last written.
	// Negative means it could not be determined.
	Idle time.Duration

	// DirtyTracked reports modified tracked files. Untracked build output
	// does not count: a verification worktree is expected to be full of it.
	DirtyTracked bool
}

// Liveness is one snapshot of the process table, taken once per sweep so that
// every worktree is judged against the same instant.
type Liveness struct {
	// Cmds maps pid to that process's current command line.
	Cmds map[int]string
}

// PIDAlive reports whether pid is running the same command it was running
// when the marker was written. The command check is what makes pid reuse
// harmless: a recycled pid running something else is not the owner.
func (l Liveness) PIDAlive(pid int, recordedCmd string) bool {
	if pid <= 0 {
		return false
	}
	cmd, ok := l.Cmds[pid]
	if !ok {
		return false
	}
	if recordedCmd == "" {
		return true
	}
	return strings.HasPrefix(cmd, recordedCmd)
}

// SessionAlive reports whether any live process names this session. The
// harness passes the session id on the command line of the process serving it
// (--session-id, or --resume of the transcript path), so a session that is
// running is a substring match away.
func (l Liveness) SessionAlive(session string) bool {
	if session == "" {
		return false
	}
	needle := strings.ToLower(session)
	for _, cmd := range l.Cmds {
		if strings.Contains(strings.ToLower(cmd), needle) {
			return true
		}
	}
	return false
}

// Policy is the dial set for one sweep.
type Policy struct {
	// RepoRoot is the clone whose worktrees are swept.
	RepoRoot string
	// Protected are absolute paths that are never removed however dead
	// their owner looks — the daemon's build snapshot lives here.
	Protected []string
	// SessionGrace is how long a session-owned worktree must have been
	// idle, on top of its session having no live process, before it is
	// reaped. It exists because an idle agent process is stopped on
	// purpose by the butler's GC and rehydrated later.
	SessionGrace time.Duration
	// IdleGrace is the same waiting period for a worktree nothing
	// identifies. Longer, because idleness is the only evidence there is.
	IdleGrace time.Duration
}

// Default grace periods. SessionGrace is comfortably longer than the gap
// between a butler idle-GC stop and the rehydrate that follows an owner
// direct; IdleGrace is longer than the slowest ratchet run, so an unmarked
// verification worktree is never reaped out from under a build in progress.
const (
	DefaultSessionGrace = 20 * time.Minute
	DefaultIdleGrace    = 60 * time.Minute
)

// WithDefaults fills unset dials. Unset is the zero value, so a Policy nobody
// configured is the conservative one rather than one that waits for nothing —
// the failure mode of the other convention is deleting a live worker's tree.
// A caller that genuinely wants no waiting period asks for a tiny one.
func (p Policy) WithDefaults() Policy {
	if p.SessionGrace == 0 {
		p.SessionGrace = DefaultSessionGrace
	}
	if p.IdleGrace == 0 {
		p.IdleGrace = DefaultIdleGrace
	}
	return p
}

// Action is what a sweep does with one worktree.
type Action int

const (
	// ActionKeep: the worktree is legitimately there.
	ActionKeep Action = iota
	// ActionRemove: its owner is gone and it can be deleted.
	ActionRemove
	// ActionHold: it looks stale but something says do not touch it. Held
	// entries are reported, never silently retained.
	ActionHold
)

func (a Action) String() string {
	switch a {
	case ActionRemove:
		return "remove"
	case ActionHold:
		return "hold"
	default:
		return "keep"
	}
}

// Decision is one verdict, always with the reason that produced it. The reason
// is the product here as much as the action is: it is what an operator reads
// when a worktree they wanted survived, or one they wanted did not.
type Decision struct {
	Action Action
	Reason string
}

// Decide is the whole policy, and it is pure: same observation, same liveness
// snapshot, same dials, same verdict. Everything that talks to the world
// happens in Sweep.
func Decide(o Observation, live Liveness, pol Policy) Decision {
	pol = pol.WithDefaults()

	switch {
	case o.Entry.Main:
		return Decision{ActionKeep, "the clone itself"}
	case o.Entry.Bare:
		return Decision{ActionKeep, "bare repository"}
	case isProtected(o.Entry.Path, pol.Protected):
		return Decision{ActionKeep, "protected path"}
	}

	// A missing directory is exactly what plain prune is for, and it is
	// safe regardless of owner: there is nothing left to lose.
	if !o.Exists {
		return Decision{ActionRemove, "worktree directory no longer exists"}
	}

	if o.Entry.Locked {
		return Decision{ActionHold, "worktree is locked"}
	}
	if !o.Entry.Detached {
		return Decision{ActionHold, fmt.Sprintf("checked out on %s, not a detached verification worktree", shortRef(o.Entry.Branch))}
	}

	switch o.Owner.Kind {
	case OwnerMarker:
		if live.PIDAlive(o.Owner.PID, o.Owner.Cmd) {
			return Decision{ActionKeep, fmt.Sprintf("owner pid %d is running", o.Owner.PID)}
		}
		// A dead pid is proof, not inference: that process is not coming
		// back, so no waiting period applies.
		return guardDirty(o, Decision{ActionRemove, fmt.Sprintf("owner pid %d is gone", o.Owner.PID)})

	case OwnerSession:
		if live.SessionAlive(o.Owner.Session) {
			return Decision{ActionKeep, fmt.Sprintf("session %s is live", short(o.Owner.Session))}
		}
		if o.Idle < 0 {
			return Decision{ActionHold, fmt.Sprintf("session %s has no live process but the tree's idle time is unknown", short(o.Owner.Session))}
		}
		if o.Idle < pol.SessionGrace {
			return Decision{ActionHold, fmt.Sprintf("session %s has no live process but the tree was written %s ago (grace %s) — an idle agent process is stopped on purpose and rehydrates", short(o.Owner.Session), round(o.Idle), round(pol.SessionGrace))}
		}
		return guardDirty(o, Decision{ActionRemove, fmt.Sprintf("session %s is gone and the tree has been idle %s", short(o.Owner.Session), round(o.Idle))})

	default:
		if o.Idle < 0 {
			return Decision{ActionHold, "no owner could be identified and the tree's idle time is unknown"}
		}
		if o.Idle < pol.IdleGrace {
			return Decision{ActionHold, fmt.Sprintf("no owner could be identified and the tree was written %s ago (grace %s)", round(o.Idle), round(pol.IdleGrace))}
		}
		return guardDirty(o, Decision{ActionRemove, fmt.Sprintf("no owner could be identified and the tree has been idle %s", round(o.Idle))})
	}
}

// guardDirty converts a removal into a held report when the worktree has
// modified tracked files. A detached verification tree should have none; if it
// does, somebody edited in there, and deleting it would destroy the only copy.
func guardDirty(o Observation, remove Decision) Decision {
	if !o.DirtyTracked {
		return remove
	}
	return Decision{ActionHold, remove.Reason + ", but it has modified tracked files — not removed"}
}

// sessionRe matches a UUID path component. The harness names each session's
// scratchpad directory after the session, so the path carries the owner even
// when nothing wrote a marker.
var sessionRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// SessionFromPath returns the session uuid a worktree path is filed under, or
// "" if none. The last matching component wins, so a nested scratchpad answers
// with the session that actually owns it.
func SessionFromPath(path string) string {
	var found string
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if sessionRe.MatchString(part) {
			found = strings.ToLower(part)
		}
	}
	return found
}

func isProtected(path string, protected []string) bool {
	clean := filepath.Clean(path)
	for _, p := range protected {
		if p != "" && filepath.Clean(p) == clean {
			return true
		}
	}
	return false
}

func shortRef(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/")
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func round(d time.Duration) time.Duration {
	if d < time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(time.Minute)
}
