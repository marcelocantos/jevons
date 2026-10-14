// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// This file is 🎯T1027: a gate id cited as evidence is checked against the
// repo of the agent citing it.
//
// # The incident
//
// 2026-10-07, 04:13Z. arrai-t41-gate-finish (workdir under
// github.com/arr-ai/arrai) reported a RED slowpath gate, id 8c015098, as its
// blocker; the daemon's FALSE-GREEN banner rode in front of that report. One
// second earlier jv-t1024-reap-waiting-worker-scout (a jevons worktree) had
// sent an in-progress report. The overseer's next turn was caused by the
// jevons report and had the arrai report in the same context, and it told
// jevons-po that jv-t1024 had cited 8c015098. It had not: none of its stored
// reports name that id before the correction arrived. No worker cited a
// foreign gate and no id collided — a supervisor reading two reports in one
// turn attached a banner to the wrong one.
//
// # Why a mechanical check anyway
//
// The store under ~/.jevons/gates is one namespace for every repo on the
// machine, and nothing in the checker asked whose repo a cited record came
// from: a worker in jevons citing 8c015098 would have been flagged only for
// the RED, never for the arrai. Had the arrai gate been GREEN it would have
// been accepted as jevons evidence. The record carries the commit it
// measured, and a commit is either in the citing repo's object store or it
// is not — that question does not depend on path spelling, survives a temp
// clone or a detached worktree, and needs no caller identity beyond the
// workdir the registry already holds.

// CommitKnownFunc reports whether the citing repository holds commit as an
// object. Tests inject fakes; production uses CommitKnownIn.
type CommitKnownFunc func(commit string) bool

// CommitKnownIn binds CommitKnownFunc to the git repository containing dir.
// ok is false when dir is not inside a git work tree, in which case there is
// no repo to scope a citation to and callers skip the check.
func CommitKnownIn(dir string) (known CommitKnownFunc, ok bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, false
	}
	if _, err := gitOut(dir, "rev-parse", "--show-toplevel"); err != nil {
		return nil, false
	}
	return func(commit string) bool {
		commit = strings.TrimSpace(commit)
		if commit == "" {
			return false
		}
		cmd := exec.Command("git", "-C", dir, "cat-file", "-e", commit+"^{commit}")
		return cmd.Run() == nil
	}, true
}

// MeasuredRepo names the repository a record's tree came from, for a reader.
// A clean run's Tree.Repo is the ephemeral worktree; the clone it was taken
// from is Shared.Repo, which is the name a human recognises.
func MeasuredRepo(rec *Record) string {
	if rec == nil || rec.Tree == nil {
		return ""
	}
	if rec.Tree.Shared != nil && rec.Tree.Shared.Repo != "" {
		return rec.Tree.Shared.Repo
	}
	return rec.Tree.Repo
}

// citedGateIDs collects every gate id a report cites, in order of first
// appearance: GATE attestation lines, the envelope gate-id slot, and
// declared gate-role ids. The evidence string is the fragment a reader can
// find the citation by.
func citedGateIDs(report string) (ids []string, evidence map[string]string) {
	evidence = map[string]string{}
	add := func(id, ev string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, seen := evidence[id]; seen {
			return
		}
		ids = append(ids, id)
		evidence[id] = ev
	}
	for _, c := range ParseAttestations(report) {
		add(c.ID, c.Raw)
	}
	if m, _ := envelope.Parse(report); m != nil && m.GateID != "" {
		add(m.GateID, "jevons: gate-id "+m.GateID)
	}
	for id := range envelope.ControlIDs(report) {
		add(id, "jevons: gate-role "+id)
	}
	return ids, evidence
}

// FlagForeignGates reports every gate the report cites whose stored record
// measured a commit the citing repository does not hold (🎯T1027).
//
// lookup resolves an id to its record; known answers for the citing repo.
// Either nil skips the check — there is nothing to compare. An id with no
// record is FlagFalseGreen's business (attestation_unknown) and is silent
// here. A record with no tree provenance is unknown, not foreign: vouching
// either way for the runs one knows least about is how a checker earns its
// silence, and the T397-era records that lack the field are exactly those.
//
// The role the citation plays is deliberately not consulted. A foreign
// gate is foreign as a control or as a falsification just as much as it is
// as a pass: none of those roles make another repo's run say anything about
// this one.
func FlagForeignGates(report string, lookup func(string) (*Record, bool), known CommitKnownFunc) []Flag {
	if lookup == nil || known == nil {
		return nil
	}
	ids, evidence := citedGateIDs(report)
	var flags []Flag
	for _, id := range ids {
		rec, ok := lookup(id)
		if !ok || rec == nil || rec.Tree == nil || rec.Tree.Commit == "" {
			continue
		}
		if known(rec.Tree.Commit) {
			continue
		}
		flags = append(flags, Flag{
			Kind:     FlagForeignRepoGate,
			Detail:   foreignDetail(id, rec),
			Evidence: evidence[id],
		})
	}
	return flags
}

func foreignDetail(id string, rec *Record) string {
	where := MeasuredRepo(rec)
	if where == "" {
		where = "an unrecorded working tree"
	}
	return fmt.Sprintf(
		"gate record %s (%s) measured commit %s in %s, which is not a commit of the citing repository — "+
			"another repo's run is not evidence for work here; run your own gate and cite its id (🎯T1027)",
		id, rec.Name, rec.Tree.ShortCommit(), where)
}

// GateScope is jevons_gate_show's answer to "is this gate mine?" when the
// caller names its own workdir (🎯T1027).
type GateScope struct {
	// Repo is the workdir the caller asked about, as given.
	Repo string `json:"repo"`
	// Commit is the commit the record measured, or "" when unknown.
	Commit string `json:"commit,omitempty"`
	// MeasuredIn is the repository the record came from, for the reader.
	MeasuredIn string `json:"measured_in,omitempty"`
	// Verdict is one of ScopeOwn, ScopeForeign, ScopeUnknown.
	Verdict ScopeVerdict `json:"verdict"`
}

// ScopeVerdict says whether a record's commit belongs to the asking repo.
type ScopeVerdict string

const (
	// ScopeOwn: the measured commit is an object of the asking repository.
	ScopeOwn ScopeVerdict = "own"
	// ScopeForeign: the asking repository has no such commit — the gate ran
	// on a different project, or on history this repo has never seen.
	ScopeForeign ScopeVerdict = "foreign"
	// ScopeUnknown: the record carries no tree provenance, or the asking
	// path is not a git work tree. Never read as own.
	ScopeUnknown ScopeVerdict = "unknown"
)

// ScopeOf classifies rec against the repository at workDir.
func ScopeOf(rec *Record, workDir string) GateScope {
	sc := GateScope{Repo: workDir, Verdict: ScopeUnknown, MeasuredIn: MeasuredRepo(rec)}
	if rec == nil || rec.Tree == nil || rec.Tree.Commit == "" {
		return sc
	}
	sc.Commit = rec.Tree.Commit
	known, ok := CommitKnownIn(workDir)
	if !ok {
		return sc
	}
	if known(rec.Tree.Commit) {
		sc.Verdict = ScopeOwn
	} else {
		sc.Verdict = ScopeForeign
	}
	return sc
}

// Describe renders the scope as the line jevons_gate_show prints.
func (sc GateScope) Describe() string {
	switch sc.Verdict {
	case ScopeOwn:
		return fmt.Sprintf("scope: own — commit %s is in %s", shortCommit(sc.Commit), sc.Repo)
	case ScopeForeign:
		return fmt.Sprintf("scope: FOREIGN — commit %s (measured in %s) is not a commit of %s; "+
			"this gate is not evidence for work there (🎯T1027)",
			shortCommit(sc.Commit), sc.MeasuredIn, sc.Repo)
	default:
		if sc.Commit == "" {
			return "scope: unknown — the record carries no tree provenance, so it cannot be tied to any repository"
		}
		return fmt.Sprintf("scope: unknown — %s is not a git work tree, so commit %s cannot be checked against it",
			sc.Repo, shortCommit(sc.Commit))
	}
}
