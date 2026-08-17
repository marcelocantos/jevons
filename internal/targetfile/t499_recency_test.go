// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import "testing"

// 🎯T499: ready frontier leaves prefer recent targets over long-lived ones.
// OrderLeavesPreferRecent is the pure pick bias: newest discovered date
// first, keyed on the ledger timestamp — not id order alone.

func TestT499OrderPrefersNewerDateOverOlderLowerID(t *testing.T) {
	// The older leaf has the HIGHER id: any id-only ordering (ascending or
	// descending) gets at most one of these two cases right, so together
	// they prove the date drives the order.
	leaves := []FrontierLeaf{
		{ID: "T700", Discovered: "2026-08-17"},
		{ID: "T650", Discovered: "2026-01-01"},
	}
	OrderLeavesPreferRecent(leaves)
	if leaves[0].ID != "T700" {
		t.Fatalf("newer-dated leaf must come first, got %v", []string{leaves[0].ID, leaves[1].ID})
	}

	leaves = []FrontierLeaf{
		{ID: "T650", Discovered: "2026-08-17"},
		{ID: "T700", Discovered: "2026-01-01"},
	}
	OrderLeavesPreferRecent(leaves)
	if leaves[0].ID != "T650" {
		t.Fatalf("newer-dated leaf must come first even with the lower id, got %v",
			[]string{leaves[0].ID, leaves[1].ID})
	}
}

func TestT499MissingDiscoveredSortsOldest(t *testing.T) {
	leaves := []FrontierLeaf{
		{ID: "T900"}, // undated: treated as longest-lived
		{ID: "T100", Discovered: "2025-02-03"},
	}
	OrderLeavesPreferRecent(leaves)
	if leaves[0].ID != "T100" || leaves[1].ID != "T900" {
		t.Fatalf("undated leaf must sort oldest, got %v", []string{leaves[0].ID, leaves[1].ID})
	}
}

func TestT499SameDayTieBreaksNaturalIDDescending(t *testing.T) {
	// Ids are minted in filing order, so among same-day filings the higher
	// natural id is the later filing. Natural, not lexicographic: T10.10
	// outranks T10.2.
	leaves := []FrontierLeaf{
		{ID: "T10.2", Discovered: "2026-08-17"},
		{ID: "T10.10", Discovered: "2026-08-17"},
	}
	OrderLeavesPreferRecent(leaves)
	if leaves[0].ID != "T10.10" {
		t.Fatalf("same-day tie must fall to higher natural id, got %v",
			[]string{leaves[0].ID, leaves[1].ID})
	}
}

func TestT499FrontierLeavesCarryDiscovered(t *testing.T) {
	ledger := `
targets:
  T1:
    name: Dated leaf
    status: identified
    discovered: 2026-08-17
  T2:
    name: Undated leaf
    status: identified
`
	leaves, err := FrontierLeaves([]byte(ledger))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierLeaf{}
	for _, l := range leaves {
		byID[l.ID] = l
	}
	if got := byID["T1"].Discovered; got != "2026-08-17" {
		t.Fatalf("T1 discovered = %q", got)
	}
	if got := byID["T2"].Discovered; got != "" {
		t.Fatalf("T2 discovered = %q, want empty", got)
	}
}
