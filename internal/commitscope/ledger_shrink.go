// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var ledgerTargetKeyLine = regexp.MustCompile(`(?m)^[^\S\n]+(T\d+(?:\.\d+)*):[^\S\n]*$`)

// MaxLedgerTargetDrop is how many bullseye target rows may disappear in one
// commit before the pre-commit hook refuses it. The 2026-10-21 T1129 hand
// merge ef5fdf35 deleted thousands of ledger rows in one commit (🎯T1134).
const MaxLedgerTargetDrop = 5

// LedgerShrinkRefusal reports whether staging bullseye.yaml would drop more
// than MaxLedgerTargetDrop target records versus HEAD.
func LedgerShrinkRefusal(head, staged []byte) (refuse bool, message string) {
	return ledgerShrinkRefusal(head, staged, "commit")
}

// LedgerShrinkLandingRefusal is the integrate / fast-forward oracle: the
// candidate tree must not lose more than MaxLedgerTargetDrop rows versus the
// shared branch tip (🎯T1144).
func LedgerShrinkLandingRefusal(baseTip, landing []byte) (refuse bool, message string) {
	return ledgerShrinkRefusal(baseTip, landing, "landing")
}

// LedgerShrinkMergeParentsRefusal runs the shrink check against every merge
// parent. Git's pre-merge-commit hook compares the staged merge result to
// HEAD (parent 1); when MERGE_HEAD is set, parent 2 must also be checked
// (🎯T1144: 08a69a88 kept a ~100KiB working-tree clobber that dropped rows
// versus both ca47c08f and 5581c6b4).
func LedgerShrinkMergeParentsRefusal(parents [][]byte, staged []byte) (refuse bool, message string) {
	for i, p := range parents {
		if len(p) == 0 {
			continue
		}
		refuse, msg := ledgerShrinkRefusal(p, staged, fmt.Sprintf("merge parent %d", i+1))
		if refuse {
			return true, msg
		}
	}
	return false, ""
}

// ledgerShrinkRefusal compares baseline vs candidate target maps.
func ledgerShrinkRefusal(baseline, candidate []byte, verb string) (refuse bool, message string) {
	h, errH := parseTargets(baseline)
	if errH != nil {
		return false, ""
	}
	s, errS := parseTargets(candidate)
	if errS != nil {
		// Incident specimens (🎯T1144) are often syntactically broken YAML
		// mid-target; still refuse when the key-line census collapses.
		headN := len(h)
		candN := countLedgerTargetKeys(candidate)
		drop := headN - candN
		if drop <= MaxLedgerTargetDrop {
			return false, ""
		}
		lost := droppedTargetIDs(h, keysFromLines(candidate))
		return true, formatLedgerShrinkMessage(verb, drop, headN, candN, lost)
	}
	drop := len(h) - len(s)
	if drop <= MaxLedgerTargetDrop {
		return false, ""
	}
	lost := droppedTargetIDs(h, s)
	return true, formatLedgerShrinkMessage(verb, drop, len(h), len(s), lost)
}

func countLedgerTargetKeys(raw []byte) int {
	return len(keysFromLines(raw))
}

// CountLedgerTargetKeys is the key-line census used when YAML is broken by a
// partial write (🎯T1144 incident specimens).
func CountLedgerTargetKeys(raw []byte) int {
	return countLedgerTargetKeys(raw)
}

func keysFromLines(raw []byte) map[string][]byte {
	keys := make(map[string][]byte)
	for _, m := range ledgerTargetKeyLine.FindAllStringSubmatch(string(raw), -1) {
		if len(m) > 1 {
			keys[m[1]] = nil
		}
	}
	return keys
}

func droppedTargetIDs(head, staged map[string][]byte) []string {
	var out []string
	for id := range head {
		if _, ok := staged[id]; !ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func formatLedgerShrinkMessage(verb string, drop, headN, stagedN int, lost []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: refusing %s — %s would drop %d target rows (%d → %d).\n\n",
		verb, SharedLedger, drop, headN, stagedN)
	b.WriteString("Lost target rows (sample):\n")
	for i, id := range lost {
		if i == MaxNamed {
			fmt.Fprintf(&b, "  … and %d more\n", len(lost)-MaxNamed)
			break
		}
		fmt.Fprintf(&b, "  %s\n", id)
	}
	fmt.Fprintf(&b, "\nA %s that shrinks the ledger by more than %d rows is almost certainly accidental\n", verb, MaxLedgerTargetDrop)
	b.WriteString("(🎯T1134/T1144: ef5fdf35 and 08a69a88 landed ~100KiB ledger clobbers). Inspect:\n")
	fmt.Fprintf(&b, "  git diff HEAD -- %s\n", SharedLedger)
	return b.String()
}
