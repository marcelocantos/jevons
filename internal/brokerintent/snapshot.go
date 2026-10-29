// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package brokerintent projects Jevons' deliberate holds into Claudia's
// consumer-state root, without mutating Jevons' real fleet intent.
package brokerintent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

const (
	maxBytes = 1 << 20
	maxHolds = 4096
	paused   = "frontier_consume_disabled"
	achieved = "target_achieved"
)

// Root is the directory passed as Claudia's --consumer-state-dir.
// Production: $HOME/.jevons/claudia-consumer; isolates use their state dir.
func Root(stateDir string) string { return filepath.Join(stateDir, "claudia-consumer") }

// Writer serialises atomic publications. Callers sample the registry and
// intent under a common refresh lock so stale snapshots cannot overtake
// newer ones. It never changes the real fleet intent or broker claims.
type Writer struct {
	mu   sync.Mutex
	root string
}

func New(stateDir string) *Writer { return &Writer{root: Root(stateDir)} }
func (w *Writer) Root() string    { return w.root }

type record struct {
	State string `json:"state"`
}
type intentDocument struct {
	Agents map[string]record `json:"agents"`
}
type policyHold struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
type policyDocument struct {
	Version   int          `json:"version"`
	Kind      string       `json:"kind"`
	UpdatedAt string       `json:"updated_at"`
	Holds     []policyHold `json:"holds"`
}

// Write publishes the two-file T233 contract. intent.json contains only
// genuine parked/reaped rows, including reaped names no longer in Jevons'
// registry. policy-hold.json contains only registered AutoStart seats held
// by frontier pause or achieved target. A lift removes the transient entry;
// the actual owner park, if any, remains in intent.json.
//
// Both files are complete atomic 0600 snapshots below private 0700 dirs.
// Publication refuses >1MiB for either file or >4096 policy entries; the
// reader independently validates untrusted files and fail-closes on errors.
func (w *Writer) Write(defs []claudia.AgentDef, intent fleetintent.Snapshot, hold func(string) string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	real := intentDocument{Agents: make(map[string]record)}
	for name, agent := range intent.Agents {
		if fleetintent.StoodDown(agent.State) {
			real.Agents[name] = record{State: string(agent.State)}
		}
	}
	policy := policyDocument{Version: 1, Kind: "policy_hold", Holds: []policyHold{}}
	seen := make(map[string]bool)
	if hold != nil {
		for _, def := range defs {
			if !def.AutoStart {
				continue
			}
			if def.Name == "" {
				return fmt.Errorf("policy hold: empty AutoStart name")
			}
			if seen[def.Name] {
				return fmt.Errorf("policy hold: duplicate name %q", def.Name)
			}
			seen[def.Name] = true
			reason := hold(def.Name)
			switch reason {
			case "", "owner_paused": // separate owner-seat pause, not T233's auto policy
				continue
			case paused, achieved:
				policy.Holds = append(policy.Holds, policyHold{Name: def.Name, Reason: reason})
			default:
				return fmt.Errorf("policy hold: unsupported reason %q for %q", reason, def.Name)
			}
			if len(policy.Holds) > maxHolds {
				return fmt.Errorf("policy hold: more than %d entries", maxHolds)
			}
		}
	}
	sort.Slice(policy.Holds, func(i, j int) bool { return policy.Holds[i].Name < policy.Holds[j].Name })
	// Preflight both payloads before replacing either snapshot, so a bad
	// policy payload cannot leave a newly published intent alongside stale
	// policy state. Each rename remains independently atomic to readers.
	intentData, err := json.Marshal(real)
	if err != nil {
		return err
	}
	if err := checkSize("intent.json", intentData); err != nil {
		return err
	}
	fleet := filepath.Join(w.root, "fleet")
	path := filepath.Join(fleet, "policy-hold.json")
	// Keep updated_at stable on an unchanged hold set; a periodic ledger
	// refresh must not fabricate a new transition each second. No age cutoff
	// is applied on the writer side.
	unchanged := false
	if old, err := os.ReadFile(path); err == nil {
		var prev policyDocument
		if json.Unmarshal(old, &prev) == nil && prev.Version == 1 && prev.Kind == policy.Kind &&
			prev.UpdatedAt != "" && validTimestamp(prev.UpdatedAt) && equalHolds(prev.Holds, policy.Holds) {
			policy.UpdatedAt = prev.UpdatedAt
			unchanged = true
		}
	}
	if !unchanged {
		policy.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if !validTimestamp(policy.UpdatedAt) {
		return fmt.Errorf("policy hold: invalid timestamp %q", policy.UpdatedAt)
	}
	policyData, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if err := checkSize(path, policyData); err != nil {
		return err
	}
	if err := privateDir(w.root); err != nil {
		return err
	}
	if err := privateDir(fleet); err != nil {
		return err
	}
	if err := publish(filepath.Join(fleet, "intent.json"), intentData); err != nil {
		return err
	}
	return publish(path, policyData)
}

func validTimestamp(s string) bool { _, err := time.Parse(time.RFC3339Nano, s); return err == nil }
func equalHolds(a, b []policyHold) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

// publish replaces only a complete fsynced 0600 snapshot. An old valid
// snapshot survives an error rather than being truncated to partial JSON.
func checkSize(path string, data []byte) error {
	if len(data) > maxBytes {
		return fmt.Errorf("consumer snapshot %s: %d bytes exceeds %d", path, len(data), maxBytes)
	}
	return nil
}

func publish(path string, data []byte) error {
	if err := checkSize(path, data); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return os.Chmod(path, 0600)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish broker consumer snapshot: %w", err)
	}
	return nil
}
