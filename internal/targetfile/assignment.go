// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Owner assignment (🎯T451).
//
// A bullseye target carries an optional `owned_by` block naming a human the
// target is parked with. Bullseye itself excludes owned targets from the
// frontier by construction: the state means "no agent should be working this,
// and no worker should exist for it". Nothing in this tree read the field —
// `grep -rn owned_by --include=*.go` returned nothing before this file — so
// every Jevons classifier that asked "is this mission still open?" answered
// yes for work that had been deliberately handed to a person.
//
// The status alone cannot answer it: an owner-parked target keeps whatever
// status it had (🎯T370 is `converging`, 🎯T383 was `identified`), because the
// engineering state and the who-holds-it state are different facts. So
// open-mission classification reads both.

// assignmentDoc is a narrow view of the ledger: status plus owner assignment.
// Deliberately separate from ledgerDoc in dedupe.go — that struct is on the
// kickoff path and several workers share this tree.
type assignmentDoc struct {
	Targets map[string]assignmentTarget `yaml:"targets"`
}

type assignmentTarget struct {
	Status  string     `yaml:"status"`
	OwnedBy *ownedBloc `yaml:"owned_by"`
}

// ownedBloc is the `owned_by` mapping. A pointer so an absent block and a
// present-but-empty one stay distinguishable, and `owned_by:` written with a
// null value (the shape an unassign leaves behind) decodes to nil rather than
// erroring.
type ownedBloc struct {
	Owner  string `yaml:"owner"`
	Reason string `yaml:"reason"`
}

// LookupTargetOwner returns the human a target is assigned to in a ledger YAML
// document. ok is false when the target is absent or carries no assignment;
// owner is the trimmed `owned_by.owner` text otherwise.
func LookupTargetOwner(data []byte, id string) (owner string, ok bool) {
	id = strings.TrimSpace(strings.TrimPrefix(id, "🎯"))
	if id == "" {
		return "", false
	}
	var doc assignmentDoc
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Targets == nil {
		return "", false
	}
	t, found := doc.Targets[id]
	if !found || t.OwnedBy == nil {
		return "", false
	}
	owner = strings.TrimSpace(t.OwnedBy.Owner)
	if owner == "" {
		return "", false
	}
	return owner, true
}

// IsOwnerAssigned reports whether id is parked with a human in the ledger
// nearest cwd. A missing ledger, an unreadable one, or an unknown row is not
// an assignment: unknown stays open, the same residual LoadTargetStatusFromCwd
// takes.
func IsOwnerAssigned(cwd, targetID string) bool {
	_, ok := LoadTargetOwnerFromCwd(cwd, targetID)
	return ok
}

// LoadTargetOwnerFromCwd looks up the owner assignment in the nearest ledger.
func LoadTargetOwnerFromCwd(cwd, targetID string) (owner string, ok bool) {
	path, err := DiscoverLedgerPath(cwd)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return LookupTargetOwner(data, targetID)
}

// MissionOpen reports whether a target is still work an agent should be doing,
// given its status and its owner assignment. Owner-assigned closes the mission
// exactly as `achieved` does (🎯T451): both mean no worker should exist here,
// they just arrive at it differently — one because the desired state is
// reached, the other because a person holds the ball.
func MissionOpen(status, owner string) bool {
	if strings.TrimSpace(owner) != "" {
		return false
	}
	return IsOpenStatus(status)
}

// MissionOpenFromCwd is MissionOpen against the ledger nearest cwd. A target
// the ledger cannot answer for stays open — the daemon must not silence a
// worker because a ledger moved.
func MissionOpenFromCwd(cwd, targetID string) bool {
	tid := strings.TrimSpace(strings.TrimPrefix(targetID, "🎯"))
	if tid == "" {
		return true
	}
	path, err := DiscoverLedgerPath(cwd)
	if err != nil {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	status, ok := LookupTargetStatus(data, tid)
	if !ok {
		return true
	}
	owner, _ := LookupTargetOwner(data, tid)
	return MissionOpen(status, owner)
}
