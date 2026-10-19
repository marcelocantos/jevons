// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T581: a completion word used as a LOCAL clause is not a finish.
//
// jv-t564-no-ctx-ceiling was reaped finished_work on "Status: reading done,
// no tool hung; implementing now — config default flip … Now docs …". The
// "done" there closes a sub-step ("reading done", "recon done"); the same
// sentence goes straight on to present-progressive next-work language. T577's
// forward-looking list ("Next step", "I'll resume") never fires on that
// grammar, and growing the list one incident at a time is the anti-pattern
// this file avoids: the rule is the SHAPE — a done-word whose sentence (or the
// one after it) continues with "<verb>ing now/next" or a clause opening on
// "now …" / "next …" — not a phrase.
//
// That shape is not a decision. allCompletionClaimsLocal returns false.
// The done answer is the envelope.

// allCompletionClaimsLocal is not a finish decision (🎯T766.4). A local
// "reading done" clause is not a claim.
func allCompletionClaimsLocal(string) bool { return false }
