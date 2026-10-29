// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/marcelocantos/jevons/internal/gate"
)

const TrunkRed = "trunk_red"
const trunkFaultFile = "jevons-trunk-fault.json"
const maxTestEventLineBytes = 1 << 20

// TrunkFault is evidence from two pristine gates, not an interpretation of
// an agent's explanation. The daemon consumes it on its existing sentinel tick.
type TrunkFault struct {
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	Base          string   `json:"base"`
	Tests         []string `json:"tests"`
	Command       []string `json:"command"`
	MergeGate     string   `json:"merge_gate"`
	BaseGate      string   `json:"base_gate"`
	OwnerNotified bool     `json:"owner_notified"`
	POWoken       bool     `json:"po_woken"`
}

func (f *TrunkFault) Error() string {
	return fmt.Sprintf("%s: trunk %s fails %s (merge gate %s, base gate %s); repair trunk before retrying unrelated landings", f.Kind, short(f.Base), strings.Join(f.Tests, ", "), f.MergeGate, f.BaseGate)
}
func (f *TrunkFault) Unwrap() error { return ErrPostMergeGate }

func trunkFaultPath(repo string) (string, error) {
	common, err := gitOut(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(common, trunkFaultFile), nil
}

func ReadTrunkFault(repo string) (*TrunkFault, error) {
	path, err := trunkFaultPath(repo)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f TrunkFault
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("read trunk fault: %w", err)
	}
	if f.Kind != TrunkRed || len(f.ID) != sha256.Size*2 || f.Base == "" || len(f.Tests) == 0 || f.BaseGate == "" || f.MergeGate == "" {
		return nil, fmt.Errorf("invalid trunk fault %s", path)
	}
	return &f, nil
}

// SaveTrunkFault publishes atomically under a short incident lock. It does not
// hold the landing lock while the sentinel delivers, so a long integration gate
// cannot starve the owner's fault notice.
func SaveTrunkFault(repo string, f *TrunkFault) error {
	unlock, err := lockTrunkFault(repo, false)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := ReadTrunkFault(repo)
	if err != nil {
		return err
	}
	if old != nil && old.ID == f.ID {
		f.OwnerNotified = f.OwnerNotified || old.OwnerNotified
		f.POWoken = f.POWoken || old.POWoken
	}
	return writeTrunkFault(repo, f)
}

func writeTrunkFault(repo string, f *TrunkFault) error {
	path, err := trunkFaultPath(repo)
	if err != nil {
		return err
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".trunk-fault-*")
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

// classifyTrunkFailure tests the exact pre-landing tree with the same test
// command. Build errors, outages, unnamed failures and merge-only failures
// remain ordinary gate failures; only common named test failures are trunk_red.
func classifyTrunkFailure(repo, baseTip, storeRoot string, merged *gate.Record) (*TrunkFault, error) {
	if merged == nil || !merged.StatusKnown || merged.ExitStatus != 1 || merged.Verdict != gate.VerdictRed {
		return nil, nil
	}
	names, err := failingTestIdentities(merged)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 || len(merged.Command) < 2 || merged.Command[0] != "go" || merged.Command[1] != "test" {
		return nil, nil
	}
	old, err := ReadTrunkFault(repo)
	if err != nil {
		return nil, err
	}
	if old != nil && old.Base == baseTip {
		for _, name := range names {
			if slices.Contains(old.Tests, name) {
				return old, nil
			}
		}
	}
	store, err := gate.OpenStore(storeRoot)
	if err != nil {
		return nil, err
	}
	result, _ := gate.RunClean(&gate.CleanArgs{Repo: repo, Commit: baseTip, Command: merged.Command, Name: "integrate-trunk-check", Store: store})
	if result == nil || result.Record == nil {
		return nil, nil
	}
	base := result.Record
	if !base.StatusKnown || base.ExitStatus != 1 || base.Verdict != gate.VerdictRed {
		return nil, nil
	}
	failed := make(map[string]bool)
	baseNames, err := failingTestIdentities(base)
	if err != nil {
		return nil, err
	}
	for _, name := range baseNames {
		failed[name] = true
	}
	var common []string
	for _, name := range names {
		if failed[name] {
			common = append(common, name)
		}
	}
	if len(common) == 0 {
		return nil, nil
	}
	sort.Strings(common)
	sum := sha256.Sum256([]byte(strings.Join(common, "\n")))
	f := &TrunkFault{Kind: TrunkRed, ID: hex.EncodeToString(sum[:]), Base: baseTip, Tests: common, Command: merged.Command, MergeGate: merged.ID, BaseGate: base.ID}
	old, err = ReadTrunkFault(repo)
	if err != nil {
		return nil, err
	}
	if old != nil && old.ID == f.ID {
		f.OwnerNotified, f.POWoken = old.OwnerNotified, old.POWoken
	}
	if err := SaveTrunkFault(repo, f); err != nil {
		return nil, err
	}
	return f, nil
}

// DeliverTrunkFault serializes delivery state with incident publication.
// A crash between delivery and acknowledgement may redeliver the same identity;
// receivers must reuse the specified fixer seat.
func DeliverTrunkFault(repo string, deliver func(*TrunkFault)) error {
	unlock, err := lockTrunkFault(repo, true)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := ReadTrunkFault(repo)
	if err != nil || current == nil {
		return err
	}
	before := *current
	deliver(current)
	if before.OwnerNotified == current.OwnerNotified && before.POWoken == current.POWoken {
		return nil
	}
	return writeTrunkFault(repo, current)
}

func lockTrunkFault(repo string, nonblocking bool) (func(), error) {
	path, err := trunkFaultPath(repo)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(path), "jevons-trunk-fault.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	mode := syscall.LOCK_EX
	if nonblocking {
		mode |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), mode); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

// resolveTrunkFault clears an incident only after its original test command
// passes on the newly landed tree. A gate on unrelated packages is not proof.
func resolveTrunkFault(repo, tip, root string, verified *gate.Record) error {
	f, err := ReadTrunkFault(repo)
	if err != nil || f == nil || len(f.Command) == 0 {
		return err
	}
	rec := verified
	if rec == nil || !slices.Equal(rec.Command, f.Command) || !rec.Verdict.IsGreen() {
		store, err := gate.OpenStore(root)
		if err != nil {
			return err
		}
		result, _ := gate.RunClean(&gate.CleanArgs{Repo: repo, Commit: tip, Command: f.Command, Name: "integrate-trunk-repair", Store: store})
		if result == nil {
			return nil
		}
		rec = result.Record
	}
	if rec == nil || !rec.Verdict.IsGreen() {
		return nil
	}
	path, err := trunkFaultPath(repo)
	if err != nil {
		return err
	}
	unlock, err := lockTrunkFault(repo, false)
	if err != nil {
		return err
	}
	defer unlock()
	return os.Remove(path)
}

// failingTestIdentities reads Go's typed test events from the immutable gate
// log. A tail, human diagnostic or legacy text log cannot establish a package
// identity and is never used to classify a branch's failure as pre-existing.
func failingTestIdentities(rec *gate.Record) ([]string, error) {
	if rec == nil || rec.OutputPath == "" {
		return nil, nil
	}
	log, err := os.Open(rec.OutputPath)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	scan := bufio.NewScanner(log)
	scan.Buffer(make([]byte, 4096), maxTestEventLineBytes)
	seen := map[string]bool{}
	var names []string
	for scan.Scan() {
		var event struct {
			Action  string
			Package string
			Test    string
		}
		if json.Unmarshal(scan.Bytes(), &event) != nil || event.Action != "fail" || event.Package == "" || event.Test == "" {
			continue
		}
		identity := event.Package + "/" + event.Test
		if !seen[identity] {
			seen[identity] = true
			names = append(names, identity)
		}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read typed test failures: %w", err)
	}
	sort.Strings(names)
	return names, nil
}
