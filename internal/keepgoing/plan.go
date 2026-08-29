// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package keepgoing is the pure T566.3 planner: reanimate a stopped
// implementer for an unblocked leaf before minting a new seat for a
// different leaf. Same load class; existing seats win.
package keepgoing

import "strings"

// Seat is one registered work implementer bound to a target.
type Seat struct {
	Name     string
	TargetID string
	Running  bool
}

// Kind is the keep-going action.
type Kind string

const (
	KindReanimate Kind = "reanimate"
	KindSpawn     Kind = "spawn"
)

// Action is one remint or new-spawn. Reanimates are listed first.
type Action struct {
	Kind     Kind
	TargetID string
	SeatName string
}

// Plan orders keep-going work: every stopped seat on a ready leaf is
// reanimated before any new spawn for a different ready leaf.
func Plan(readyIDs []string, seats []Seat) []Action {
	ready := map[string]bool{}
	order := make([]string, 0, len(readyIDs))
	for _, id := range readyIDs {
		id = strings.TrimSpace(id)
		if id == "" || ready[id] {
			continue
		}
		ready[id] = true
		order = append(order, id)
	}
	var remint, spawn []Action
	claimed := map[string]bool{}
	for _, id := range order {
		var stopped string
		running := false
		for _, s := range seats {
			if strings.TrimSpace(s.TargetID) != id {
				continue
			}
			if s.Running {
				running = true
				break
			}
			if stopped == "" {
				stopped = strings.TrimSpace(s.Name)
			}
		}
		if running {
			continue
		}
		if stopped != "" {
			remint = append(remint, Action{Kind: KindReanimate, TargetID: id, SeatName: stopped})
			claimed[id] = true
			continue
		}
		spawn = append(spawn, Action{Kind: KindSpawn, TargetID: id})
	}
	return append(remint, spawn...)
}
