// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T1237 — integrate and workers each gate a package subset (whatever the
// landing touched); nothing runs the whole module against the shared tip.
// 2026-10-28's `go test ./...` on steer was RED on four tests no subset gate
// had exercised. This is the standing net: a periodic, debounced,
// heavy-lease-serialised `go test ./...` against the tip, reusing the
// existing TrunkFault evidence/delivery path (🎯T1122) so a RED result is
// surfaced exactly once per tip, not silently dropped and not re-filed every
// tick.

const wholeModuleGateStateFile = "jevons-whole-module-gate.json"

// WholeModuleGateCommand is the standing gate's argv. A var, not a const,
// so tests substitute a scripted command instead of a real `go test ./...`.
var WholeModuleGateCommand = []string{"go", "test", "-json", "-count=1", "./..."}

// WholeModuleGateEnv is what `make test-go` exports and a bare `go test`
// from the daemon would not. Without CLAUDIA_NO_BROKER a fixture's Registry
// launch on the development machine reaches the live claudia broker and is
// granted a real seat with a real provider process behind it.
var WholeModuleGateEnv = []string{"CLAUDIA_NO_BROKER=1"}

// wholeModuleGateState is the debounce/dedup record: Candidate is a tip seen
// once but not yet confirmed stable across a tick, and GatedTip is the last
// tip this gate actually ran against (never re-run for the same tip).
type wholeModuleGateState struct {
	Candidate string `json:"candidate,omitempty"`
	GatedTip  string `json:"gated_tip,omitempty"`
}

func wholeModuleGateStatePath(repo string) (string, error) {
	common, err := gitOut(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(common, wholeModuleGateStateFile), nil
}

func readWholeModuleGateState(repo string) (*wholeModuleGateState, error) {
	path, err := wholeModuleGateStatePath(repo)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &wholeModuleGateState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var st wholeModuleGateState
	if err := json.Unmarshal(b, &st); err != nil {
		return &wholeModuleGateState{}, nil
	}
	return &st, nil
}

func writeWholeModuleGateState(repo string, st *wholeModuleGateState) error {
	path, err := wholeModuleGateStatePath(repo)
	if err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".whole-module-gate-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}

// wholeModuleGateRunning serialises standing-gate runs within this process:
// one daemon, one background gate at a time, so a slow tick never piles up
// a second `go test ./...` queued behind the first on the heavy lease.
var wholeModuleGateRunning sync.Mutex

// SettleWholeModuleGateTip reports whether tip has now been seen stable
// across two calls (debounce for "after landings settle") and is not the
// tip already gated. It is pure state-file bookkeeping, no test run, so a
// sentinel tick can call it cheaply every cycle; the caller runs the gate
// only when this returns true.
func SettleWholeModuleGateTip(repo, tip string) (bool, error) {
	if tip == "" {
		return false, fmt.Errorf("whole-module gate: empty tip")
	}
	st, err := readWholeModuleGateState(repo)
	if err != nil {
		return false, err
	}
	if tip == st.GatedTip {
		return false, nil // already gated; dedup once per tip
	}
	if tip != st.Candidate {
		st.Candidate = tip
		return false, writeWholeModuleGateState(repo, st)
	}
	return true, nil // same tip seen twice in a row: settled
}

// RunWholeModuleGate runs the standing whole-module gate against tip under
// the host-pressure heavy lease and records tip as gated regardless of
// verdict, so a RED result is reported but never retried every tick; the
// next distinct tip gets its own run. On RED it publishes (or reuses) a
// TrunkFault via the existing 🎯T1122 evidence/delivery path, so the
// sentinel's tickTrunkFault notifies the owner and wakes the PO exactly
// as it already does for integrate's post-merge gate. On GREEN it clears
// a fault whose command matches this gate's command.
func RunWholeModuleGate(repo, storeRoot, tip string) (*gate.Record, error) {
	if !wholeModuleGateRunning.TryLock() {
		return nil, nil // another run already in flight; this tick is a no-op
	}
	defer wholeModuleGateRunning.Unlock()

	// gate.Run already serialises any HeavyCommand (go test, …) on the 🎯T603
	// heavy lease whenever Store is set; acquiring it again here would
	// deadlock against that same flock within this process. The in-process
	// wholeModuleGateRunning mutex above is this function's own
	// serialisation, orthogonal to the cross-process host-pressure lease.
	store, err := gate.OpenStore(storeRoot)
	if err != nil {
		return nil, fmt.Errorf("whole-module gate: %w", err)
	}
	result, err := gate.RunClean(&gate.CleanArgs{
		Repo:    repo,
		Commit:  tip,
		Command: WholeModuleGateCommand,
		Env:     WholeModuleGateEnv,
		Name:    "whole-module-gate",
		Store:   store,
	})
	if err != nil {
		return nil, fmt.Errorf("whole-module gate: %w", err)
	}
	rec := result.Record

	st, err := readWholeModuleGateState(repo)
	if err != nil {
		return rec, err
	}
	st.GatedTip = tip
	st.Candidate = ""
	if err := writeWholeModuleGateState(repo, st); err != nil {
		return rec, err
	}

	if rec == nil || !rec.StatusKnown {
		return rec, nil
	}
	if rec.Verdict.IsGreen() {
		if err := resolveTrunkFault(repo, tip, storeRoot, rec); err != nil {
			return rec, err
		}
		return rec, nil
	}
	if rec.ExitStatus != 1 || rec.Verdict != gate.VerdictRed {
		return rec, nil // build error, outage, etc.: an ordinary gate failure, not a trunk fault
	}
	names, err := failingTestIdentities(rec)
	if err != nil || len(names) == 0 {
		return rec, err
	}
	sort.Strings(names)
	id := wholeModuleFaultID(names)
	f := &TrunkFault{
		Kind:      TrunkRed,
		ID:        id,
		Base:      tip,
		Tests:     names,
		Command:   rec.Command,
		MergeGate: rec.ID,
		BaseGate:  rec.ID,
	}
	if err := SaveTrunkFault(repo, f); err != nil {
		return rec, err
	}
	return rec, nil
}

func wholeModuleFaultID(names []string) string {
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return hex.EncodeToString(sum[:])
}
