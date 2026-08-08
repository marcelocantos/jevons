// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T262.2: worker-per-frontier-leaf multi-agent map stays pinned in the
// design packet — product map rows, policy-on-set classification, and a
// recommendation for each residual.
package docratchet_test

import (
	"strings"
	"testing"
)

// TestT262FrontierDesignPacket ratchets docs/design/frontier-as-ready-set.md
// against the T262.2 acceptance: the map to existing product, the
// policy-on-set classification (not a global queue), and per-residual
// dispositions. Not a live spawn; not an unpark of T254.
func TestT262FrontierDesignPacket(t *testing.T) {
	body := readRepo(t, "docs/design/frontier-as-ready-set.md")

	// Acceptance 1: map worker-per-leaf to existing product + residual gaps.
	productMap := []string{
		"T155", // unattended kick-off
		"T193", // file→spawn same turn
		"T198", // target_id engagement overlay
		"T222", // exclusive engage + near-dup gate
		"worker per leaf",
		"Residual recommendations",
	}
	for _, m := range productMap {
		if !strings.Contains(body, m) {
			t.Errorf("design packet missing product-map marker %q", m)
		}
	}

	// Acceptance 2: the four policy dimensions are policy-on-set, never a
	// reason to restore a global queue.
	policyOnSet := []string{
		"policy-on-set",
		"not reasons to restore a global queue",
		"shared-file ownership",
		"design/parked/needs-owner filters",
		"frontier churn",
	}
	for _, m := range policyOnSet {
		if !strings.Contains(body, m) {
			t.Errorf("design packet missing policy-on-set marker %q", m)
		}
	}

	// Acceptance 3: every residual carries a disposition from the fixed
	// category set, and each T254 child is named somewhere in the packet.
	dispositions := []string{
		"implement under T254.*",
		"new leaf",
		"already sufficient",
		"defer",
		"No residual requires a **new leaf**",
	}
	for _, m := range dispositions {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(m)) {
			t.Errorf("design packet missing residual-disposition marker %q", m)
		}
	}
	for _, child := range []string{"T254.1", "T254.2", "T254.3", "T254.4", "T254.5", "T254.6"} {
		if !strings.Contains(body, child) {
			t.Errorf("design packet missing T254 child %q in residual map", child)
		}
	}

	// Non-claims: the packet must keep saying it does not unpark T254 and
	// does not achieve the owner gate.
	for _, m := range []string{
		"does **not** unpark T254",
		"does **not** achieve T262.4",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("design packet missing non-claim marker %q", m)
		}
	}
}

// TestT262CrossProductOwnership ratchets the 🎯T262.3 packet: every
// Beads-ish surface is assigned to Bullseye / Jevons / neither, the
// per-product candidate lists stay explicit, and the out-of-scope list
// (including no Beads dual-write) survives. Not an implementation of any
// candidate; not an unpark of T254.
func TestT262CrossProductOwnership(t *testing.T) {
	body := readRepo(t, "docs/design/frontier-as-ready-set.md")

	// Acceptance 1: the ownership table exists with the three-way split
	// and rules Beads dual-write out rather than planning it.
	table := []string{
		"| Bullseye ledger | Jevons factory | Neither |",
		"Dual-write Beads ledger",
		"Beads dual-write",
		"\"Next ticket\" total order",
		"**Must not claim**",
	}
	for _, m := range table {
		if !strings.Contains(body, m) {
			t.Errorf("ownership table missing marker %q", m)
		}
	}

	// Acceptance 2: bullseye-side candidates — next-ticket UX language,
	// assign/unassign semantics, ranking as optional policy not permission.
	bullseyeSide := []string{
		"Bullseye-side candidates",
		"next most-valuable",
		"assign/unassign",
		"exclusion ≠ queue head",
		"Optional policy signal",
		"not permission",
	}
	for _, m := range bullseyeSide {
		if !strings.Contains(body, m) {
			t.Errorf("bullseye-side candidates missing marker %q", m)
		}
	}

	// Acceptance 3: jevons-side candidates — spawn caps, engagement UI,
	// factory continuity without a Beads mail bus.
	jevonsSide := []string{
		"Jevons-side candidates",
		"Spawn caps",
		"Engagement UI",
		"Factory continuity without Beads mail",
	}
	for _, m := range jevonsSide {
		if !strings.Contains(body, m) {
			t.Errorf("jevons-side candidates missing marker %q", m)
		}
	}

	// Acceptance 4: the out-of-scope list stays explicit.
	outOfScope := []string{
		"### Out of scope",
		"second ledger",
		"Unparking T254",
		"Changing bullseye graph math",
	}
	for _, m := range outOfScope {
		if !strings.Contains(body, m) {
			t.Errorf("out-of-scope list missing marker %q", m)
		}
	}
}
