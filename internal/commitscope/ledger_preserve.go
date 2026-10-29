// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"bytes"
	"fmt"
	"sort"
)

// TargetIDsDifferingFrom returns target ids whose canonical record in other
// differs from head. Missing ids in head count as differing when present in
// other (🎯T1147: uncommitted achievements and filed rows).
func TargetIDsDifferingFrom(head, other []byte) ([]string, error) {
	h, err := parseTargets(head)
	if err != nil {
		return nil, err
	}
	o, err := parseTargets(other)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(h)+len(o))
	for id := range h {
		ids[id] = struct{}{}
	}
	for id := range o {
		ids[id] = struct{}{}
	}
	var out []string
	for id := range ids {
		if !bytes.Equal(h[id], o[id]) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// UnionPreservingWorkingDivergence merges candidate into head, then overlays
// every target row that differs between working and head. Wholesale rewriters
// (restore, integrate tree update, stale in-memory apply) call this instead of
// replacing the file (🎯T1147).
func UnionPreservingWorkingDivergence(head, working, candidate []byte) ([]byte, error) {
	step, err := UnionLedgerYAML(head, candidate)
	if err != nil {
		return nil, fmt.Errorf("ledger preserve: candidate union: %w", err)
	}
	if len(bytes.TrimSpace(working)) == 0 {
		return step, nil
	}
	divergent, err := TargetIDsDifferingFrom(head, working)
	if err != nil {
		return nil, fmt.Errorf("ledger preserve: divergent scan: %w", err)
	}
	if len(divergent) == 0 {
		return step, nil
	}
	out, err := UnionLedgerYAML(step, working)
	if err != nil {
		return nil, fmt.Errorf("ledger preserve: working overlay: %w", err)
	}
	return out, nil
}

// WouldDropWorkingDivergence reports whether candidate would discard any target
// row that currently differs from head in working.
func WouldDropWorkingDivergence(head, working, candidate []byte) (bool, []string, error) {
	divergent, err := TargetIDsDifferingFrom(head, working)
	if err != nil {
		return false, nil, err
	}
	if len(divergent) == 0 {
		return false, nil, nil
	}
	merged, err := UnionPreservingWorkingDivergence(head, working, candidate)
	if err != nil {
		return false, nil, err
	}
	cand, err := parseTargets(candidate)
	if err != nil {
		return false, nil, err
	}
	mergedT, err := parseTargets(merged)
	if err != nil {
		return false, nil, err
	}
	var dropped []string
	for _, id := range divergent {
		if !bytes.Equal(cand[id], mergedT[id]) {
			dropped = append(dropped, id)
		}
	}
	return len(dropped) > 0, dropped, nil
}

// FormatWorkingDivergenceRefusal is the agent-facing text when a rewrite would
// drop uncommitted ledger rows (🎯T1147).
func FormatWorkingDivergenceRefusal(verb string, ids []string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "commitscope: refusing %s — %s would drop %d uncommitted target row(s) that differ from HEAD.\n\n",
		verb, SharedLedger, len(ids))
	b.WriteString("Rows at risk (sample):\n")
	for i, id := range ids {
		if i == MaxNamed {
			fmt.Fprintf(&b, "  … and %d more\n", len(ids)-MaxNamed)
			break
		}
		fmt.Fprintf(&b, "  %s\n", id)
	}
	b.WriteString("\nCommit the ledger row first (`git commit --only ")
	b.WriteString(SharedLedger)
	b.WriteString(" -m '...'`), or merge rather than replace. 🎯T1147\n")
	return b.String()
}
