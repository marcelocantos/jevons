// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover

import "strings"

// HasMintIdentity reports whether this pending handover carries enough of
// the predecessor's identity to rebuild a registry row when the named
// row is gone (🎯T474). Pre-T474 records lack these fields and must not
// be treated as recoverable — falling through to a genuine aside mint is
// safer than inventing a half-identity.
func (p Pending) HasMintIdentity() bool {
	if p.Delivered {
		return false
	}
	if strings.TrimSpace(p.NewSessionID) == "" {
		return false
	}
	// Purpose or WorkDir is enough to refuse the aside default; a rotation
	// that persisted neither is not a recoverable work seat.
	return strings.TrimSpace(p.Purpose) != "" || strings.TrimSpace(p.WorkDir) != ""
}

// BlocksReap reports whether an undelivered rotation for this agent must
// keep the registry row alive for the rotate→launch→seed span (🎯T474).
// A delivered seed no longer holds the name: the successor is up and a
// finished-work reap is a normal hygiene decision again.
func (p Pending) BlocksReap() bool {
	return !p.Delivered && strings.TrimSpace(p.Agent) != ""
}

// FindBlockingRotation returns the undelivered pending handover for name,
// if any. Pure over a snapshot so reap and mint callers share one rule.
func FindBlockingRotation(pending []Pending, name string) (Pending, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Pending{}, false
	}
	for _, p := range pending {
		if strings.TrimSpace(p.Agent) != name {
			continue
		}
		if p.BlocksReap() {
			return p, true
		}
	}
	return Pending{}, false
}
