// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// This file is 🎯T1107: cmd/integrate fast-forwards anything a worker
// branch's tip resolves to, whatever the gate store says about it. Three
// worker branches landed in one session (🎯T1099, 🎯T1102, 🎯T1087); only one
// of the three had a GREEN gate record naming its commit, and nothing at the
// landing step noticed. 🎯T386/T396 make the GATE line the only citable
// pass; RequireLandingGate is where that gets enforced at the one place a
// worker's commits actually reach the shared branch.
//
// # The mechanism
//
// Before Integrate lands a worker's branch, the caller (cmd/integrate) asks
// RequireLandingGate whether the gate store holds a GREEN record that
// measured the worker branch's tip — or a commit with the same tree, so a
// gate run taken in a clean detached checkout of the same change still
// counts. A worker branch whose tip cites a gate id (a GATE line in its HEAD
// commit message) is held to that citation specifically: the cited record
// must exist, must be GREEN, and must measure the cited tip, or the landing
// is refused for naming a record that does not back the claim — not merely
// for lacking one.
//
// overrideReason, when set, is the owner's escape hatch: the landing
// proceeds without a gate record, and the bypass is appended to a log in the
// shared clone's git dir rather than vanishing the moment the process exits.

// ErrNoGateRecord is returned by RequireLandingGate when the gate store has
// no GREEN record measuring the worker branch's tip (or a tree-equal
// commit), and no override was given.
var ErrNoGateRecord = errors.New("no GREEN gate record names the worker branch tip")

// ErrCitedGateMismatch is returned when the worker branch's tip commit cites
// a gate id whose record does not back the citation: missing, not GREEN, or
// measuring a different commit/tree.
var ErrCitedGateMismatch = errors.New("cited gate record does not back the worker branch tip")

// overrideLogName is the file, inside the shared clone's git common dir,
// that records every landing that skipped the gate check and why.
const overrideLogName = "jevons-integrate-overrides.log"

// RequireLandingGate refuses a landing that has no GREEN gate record behind
// it. base is the shared clone (the same BaseWorkdir Integrate takes);
// agentName and gateStoreRoot name the worker and the gate store to check
// (gateStoreRoot empty means gate.DefaultStoreRoot()).
//
// On success it returns the record that justified the landing — nil when
// the check was bypassed by overrideReason. On refusal the error names the
// worker branch's tip and either the missing/non-green record or the
// mismatched citation, so the caller's message is specific rather than a
// bare "refused".
func RequireLandingGate(base, agentName, gateStoreRoot, overrideReason string) (*gate.Record, error) {
	workerBranch := BranchName(agentName)
	workerTip, err := gitOut(base, "rev-parse", "--verify", "refs/heads/"+workerBranch)
	if err != nil {
		return nil, fmt.Errorf("integrate: resolve %s: %w", workerBranch, err)
	}

	store, err := gate.OpenStore(gateStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("integrate: gate store: %w", err)
	}

	if cited, ok, cerr := citedGateFor(base, store, workerTip); ok || cerr != nil {
		if cerr != nil {
			return nil, fmt.Errorf("integrate: %s: HEAD %s: %w", agentName, short(workerTip), cerr)
		}
		return cited, nil
	}

	if rec, err := latestGreenRecordForCommit(store, base, workerTip); err != nil {
		return nil, fmt.Errorf("integrate: gate lookup: %w", err)
	} else if rec != nil {
		return rec, nil
	}

	if strings.TrimSpace(overrideReason) != "" {
		if err := logGateOverride(base, agentName, workerTip, overrideReason); err != nil {
			return nil, err
		}
		return nil, nil
	}

	return nil, fmt.Errorf("integrate: %s: no GREEN gate record names tip %s (store %s): %w",
		agentName, short(workerTip), store.Root, ErrNoGateRecord)
}

// citedGateFor looks for a GATE attestation in the worker branch tip's own
// commit message and, when one is found, holds the landing to exactly that
// citation (🎯T1107 acceptance 2): ok is true only when the cited record
// backs the tip; a non-nil error names why it does not. ok, nil, nil means
// the tip cites nothing, so the caller falls back to scanning for any
// matching GREEN record.
func citedGateFor(base string, store *gate.Store, workerTip string) (rec *gate.Record, ok bool, err error) {
	msg, gerr := gitOut(base, "log", "-1", "--format=%B", workerTip)
	if gerr != nil {
		return nil, false, nil // no commit message to read is not a citation
	}
	cited := gate.ParseAttestations(msg)
	if len(cited) == 0 {
		return nil, false, nil
	}
	c := cited[0]
	found, exists := store.Lookup(c.ID)
	if !exists {
		return nil, false, fmt.Errorf("cites gate id %s with no record in the store: %w", c.ID, ErrCitedGateMismatch)
	}
	if !found.Verdict.IsGreen() {
		return nil, false, fmt.Errorf("cites gate id %s, recorded verdict %s, not GREEN: %w",
			c.ID, found.Verdict, ErrCitedGateMismatch)
	}
	if sameOrTreeEqual(base, found, workerTip) {
		return found, true, nil
	}
	measured := "unknown commit"
	if found.Tree != nil && found.Tree.Commit != "" {
		measured = short(found.Tree.Commit)
	}
	return nil, false, fmt.Errorf("cites gate id %s, which measured %s, not the tip %s: %w",
		c.ID, measured, short(workerTip), ErrCitedGateMismatch)
}

// latestGreenRecordForCommit scans the store for the most recent GREEN
// record whose measured commit is commit, or shares commit's tree (a
// tree-equal commit — the same change, committed under a different SHA).
// nil, nil means none was found; it is not an error for there to be none.
func latestGreenRecordForCommit(store *gate.Store, workdir, commit string) (*gate.Record, error) {
	recs, err := store.Recent(0)
	if err != nil {
		return nil, err
	}
	tree, terr := gitOut(workdir, "rev-parse", commit+"^{tree}")
	var best *gate.Record
	for _, rec := range recs {
		if !rec.Verdict.IsGreen() || rec.Tree == nil || rec.Tree.Commit == "" {
			continue
		}
		if sameOrTreeEqualTo(workdir, rec, commit, tree, terr) {
			if best == nil || rec.Started.After(best.Started) {
				best = rec
			}
		}
	}
	return best, nil
}

// sameOrTreeEqual reports whether rec's measured commit is commit itself or
// a commit with the same tree.
func sameOrTreeEqual(workdir string, rec *gate.Record, commit string) bool {
	tree, terr := gitOut(workdir, "rev-parse", commit+"^{tree}")
	return sameOrTreeEqualTo(workdir, rec, commit, tree, terr)
}

func sameOrTreeEqualTo(workdir string, rec *gate.Record, commit, commitTree string, treeErr error) bool {
	if rec.Tree.Commit == commit {
		return true
	}
	if treeErr != nil || commitTree == "" {
		return false
	}
	recTree, err := gitOut(workdir, "rev-parse", rec.Tree.Commit+"^{tree}")
	return err == nil && recTree == commitTree
}

// logGateOverride appends one line to the shared clone's override log,
// naming who bypassed the gate precondition, for which worker, at which
// tip, and why. The log lives in the git common dir so every worktree of
// the repo shares it, the same way the landing lock does.
func logGateOverride(base, agentName, workerTip, reason string) error {
	common, err := gitOut(base, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("integrate: override log: %w", err)
	}
	line := fmt.Sprintf("%s agent=%s tip=%s reason=%s\n",
		time.Now().UTC().Format(time.RFC3339), agentName, workerTip, strings.TrimSpace(reason))
	f, err := os.OpenFile(filepath.Join(common, overrideLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("integrate: override log: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(line)
	if err != nil {
		return fmt.Errorf("integrate: override log: %w", err)
	}
	return nil
}
