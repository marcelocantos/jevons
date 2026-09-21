// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"strings"
)

// ScanRegion is the text a string-matching false-green rule may read (🎯T742).
// The discrimination — prose that names a hazard versus output that exhibits
// it — lives here, not inside each matcher.
type ScanRegion string

const (
	// RegionQuoted is exhibited command/output: indented code (four spaces
	// or a tab) and fenced code blocks. Substring hazard rules scan only this.
	RegionQuoted ScanRegion = "quoted"
	// RegionShaped is the report after honest-red framing is blanked. The
	// matcher itself requires output shape (ScanOutput, 🎯T737).
	RegionShaped ScanRegion = "shaped"
)

// hazardRule is one false-green check that matches on literal hazard text.
type hazardRule struct {
	kind   FlagKind
	region ScanRegion
	scan   func(scoped string) []Flag
}

// hazardRules is the closed set of string-matching false-green rules.
// A rule that is not in this table cannot scan report text for a hazard
// substring. RegionQuoted and RegionShaped are the only legal regions;
// unbounded whole-report scan is not a value this table can hold.
var hazardRules = []hazardRule{
	{kind: FlagPipelineMasked, region: RegionQuoted, scan: scanPipelineMasked},
	{kind: FlagShellArrayTrap, region: RegionQuoted, scan: scanShellArrayTrap},
	{kind: FlagEmptyStatus, region: RegionQuoted, scan: scanEmptyStatus},
	{kind: FlagOutputContradicts, region: RegionShaped, scan: scanOutputContradicts},
}

// structuredFlagKinds are false-green flags that parse GATE lines or consult
// the store / git. They do not match on hazard substrings, so they read the
// whole report. Every FlagKind is either here or in hazardRules (🎯T742).
var structuredFlagKinds = []FlagKind{
	FlagAttestationNotGreen,
	FlagAttestationUnknown,
	FlagAttestationContradicted,
	FlagAttestationKilled,
	FlagDirtyTreeGate,
	FlagSHAUnreachable,
	FlagAttestationEmptyPackage,
	// 🎯T765 ledger-achieve flags. They never scan a finish report: CheckAchieve
	// runs on a ledger attestation, which is a claim rather than narrative.
	// Precedes-fix and tree-unknown read the record and git; uncited fires on
	// the absence of any GATE id= in that claim, not on a hazard substring.
	FlagAchieveGateUncited,
	FlagAchieveGatePrecedesFix,
	FlagAchieveTreeUnknown,
}

func scanHazards(quoted, shaped string) []Flag {
	var flags []Flag
	for _, r := range hazardRules {
		src := quoted
		if r.region == RegionShaped {
			src = shaped
		}
		flags = append(flags, r.scan(src)...)
	}
	return flags
}

func scanPipelineMasked(scoped string) []Flag {
	m := pipedGateRe.FindString(scoped)
	if m == "" || !citesAStatus(strings.ToLower(scoped)) {
		return nil
	}
	return []Flag{{
		Kind: FlagPipelineMasked,
		Detail: "the gate was piped into another command, so the status cited " +
			"is that last command's, not the gate's — run it under bin/gate instead",
		Evidence: strings.TrimSpace(m),
	}}
}

func scanShellArrayTrap(scoped string) []Flag {
	var flags []Flag
	if m := bashArrayRe.FindString(scoped); m != "" {
		flags = append(flags, Flag{
			Kind: FlagShellArrayTrap,
			Detail: "PIPESTATUS is bash-only and this harness runs zsh, where the " +
				"expansion is empty — the status was never read",
			Evidence: statusLineAround(scoped, m),
		})
	}
	if m := zshZeroIndexRe.FindString(scoped); m != "" {
		flags = append(flags, Flag{
			Kind: FlagShellArrayTrap,
			Detail: "zsh arrays index from 1, so ${pipestatus[0]} is empty — " +
				"the status was never read",
			Evidence: statusLineAround(scoped, m),
		})
	}
	return flags
}

func scanEmptyStatus(scoped string) []Flag {
	m := emptyStatusRe.FindString(scoped)
	if m == "" {
		return nil
	}
	return []Flag{{
		Kind:     FlagEmptyStatus,
		Detail:   "a status variable expanded to nothing; an empty status is not zero",
		Evidence: strings.TrimSpace(m),
	}}
}

func scanOutputContradicts(scoped string) []Flag {
	var flags []Flag
	for _, a := range ScanOutput(scoped) {
		flags = append(flags, Flag{
			Kind: FlagOutputContradicts,
			Detail: fmt.Sprintf(
				"the report claims a pass while quoting output that says otherwise (%s)",
				a.Marker),
			Evidence: a.Line,
		})
	}
	return flags
}

// quotedRegion returns the exhibited command/output in a finish report:
// indented code (markdown's four-space / tab convention) and fenced code
// blocks. Unindented prose, including inline backticks that name a hazard,
// is not in the region.
func quotedRegion(text string) string {
	var b strings.Builder
	inFence := false
	var marker string
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if !inFence {
			if m := fenceMarker(trim); m != "" {
				inFence = true
				marker = m
				continue
			}
			if isIndentedCode(line) {
				b.WriteString(line)
				b.WriteByte('\n')
			}
			continue
		}
		if strings.HasPrefix(trim, marker) {
			inFence = false
			marker = ""
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func fenceMarker(trim string) string {
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(trim, m) {
			return m
		}
	}
	return ""
}

func isIndentedCode(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	if strings.HasPrefix(line, "\t") {
		return true
	}
	return len(line) >= 4 && line[0] == ' ' && line[1] == ' ' && line[2] == ' ' && line[3] == ' '
}
