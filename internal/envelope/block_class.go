// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package envelope

// BlockClass describes who can resolve a blocked finish-report. A missing
// class remains an ordinary external block, never an inferred owner question.
type BlockClass string

const (
	BlockUnspecified   BlockClass = ""
	BlockExternal      BlockClass = "external"
	BlockOwnerDecision BlockClass = "owner-decision"
)

func ParseBlockClass(s string) (BlockClass, bool) {
	switch BlockClass(s) {
	case BlockUnspecified, BlockExternal, BlockOwnerDecision:
		return BlockClass(s), true
	default:
		return "", false
	}
}
