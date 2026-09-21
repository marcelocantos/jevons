// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"strings"
)

// AchieveField names the part of the evidence an achieve flag fails on, so a
// refusal tells the writer which claim to fix rather than only that one is bad
// (🎯T765.1). The names are the gate record's own fields.
func AchieveField(k FlagKind) string {
	switch k {
	case FlagAttestationUnknown, FlagAchieveGateUncited:
		return "gate_id"
	case FlagAttestationContradicted, FlagAttestationNotGreen:
		return "verdict"
	case FlagAchieveTreeUnknown:
		return "tree"
	case FlagDirtyTreeGate:
		return "tree.clean"
	case FlagAchieveGatePrecedesFix:
		return "tree.commit"
	}
	return "attestation"
}

// Detailed renders the result with each flag's kind and failing field, the
// form a verifier prints for a caller that needs to act on a refusal. A marked
// result leads with AchieveMarker, so it cannot be read as a verified one.
func (r AchieveResult) Detailed() string {
	var b strings.Builder
	if r.Verdict == AchieveMarked {
		fmt.Fprintf(&b, "%s: %s", AchieveMarker, r.Risk)
	} else {
		b.WriteString(string(r.Verdict))
	}
	for _, f := range r.Flags {
		fmt.Fprintf(&b, "\n  • %s [field=%s]: %s", f.Kind, AchieveField(f.Kind), f.Detail)
	}
	return b.String()
}
