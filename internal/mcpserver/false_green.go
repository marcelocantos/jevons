// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"sync"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T386: the overseer is the independent gate for oracle-first completion
// (🎯T31.1), but it can only judge the evidence a worker chose to send. When
// that evidence contradicts itself — a green claimed over a timeout panic, a
// status read off a pipeline, a shell array that expanded to nothing — the
// contradiction is decidable here, on the way past, and saying so costs one
// banner. Two reports in one session nearly retired targets on such a green.
//
// Delivery is unchanged: the report still reaches the overseer whole, with
// the flags in front of it. That banner is the only consumer of the prose
// rules in gate.FlagFalseGreen (GATE lines, pipeline and shell-array
// hazards, failure and removal framing): they are report-time advice to a
// reader who will judge the report.
//
// Auto-reap is a control, and a control does not decide by English
// (🎯T766.4.1). Its 🎯T470 veto is reapGateVeto (reap_gate_veto.go), which
// reads typed facts only: the envelope's declared slots and the gate record
// those slots name. A report whose evidence lives only in its prose is not
// a declared finish in the first place, so it is not reaped either.

// gateStore is opened once for the daemon's lifetime. A store that cannot be
// opened degrades to textual checking, which needs no disk: the flags that
// matter most (pipeline masking, the zsh array trap, output that contradicts
// the claim) are all decidable from the report alone.
var (
	gateStoreMu     sync.Mutex
	gateStoreCached *gate.Store
)

// gateStore opens the record store at gate.DefaultStoreRoot, reusing the
// handle while that root is unchanged. Keyed by root rather than latched once
// per process, so a test that points JEVONS_GATE_DIR at a hermetic store is
// honoured even when an earlier test in the same binary resolved the default
// first — otherwise the check reads whatever ~/.jevons/gates the host has.
func gateStore() *gate.Store {
	root := gate.DefaultStoreRoot()
	gateStoreMu.Lock()
	defer gateStoreMu.Unlock()
	if gateStoreCached != nil && gateStoreCached.Root == root {
		return gateStoreCached
	}
	store, err := gate.OpenStore(root)
	if err != nil {
		return nil
	}
	gateStoreCached = store
	return store
}

// FalseGreenFlags reports the ways a worker's finish report fails to support
// the pass it claims. Empty for the overwhelming majority of reports.
func FalseGreenFlags(report string) []gate.Flag {
	var lookup func(string) (*gate.Record, bool)
	if store := gateStore(); store != nil {
		lookup = store.Lookup
	}
	var flags []gate.Flag
	if m, err := envelope.Parse(report); m != nil {
		if err == nil && m.GateID != "" && m.Verdict.IsPass() && !envelope.ControlIDs(report)[m.GateID] {
			flags = append(flags, envelopeGateFlags(m.GateID, lookup)...)
		}
	}
	// Full report, not payload-only: 🎯T722 gate-role slots live in the
	// fence, and FlagFalseGreen reads them. GATE lines in the payload
	// are still in this text.
	return append(flags, gate.FlagFalseGreen(report, lookup)...)
}

func envelopeGateFlags(id string, lookup func(string) (*gate.Record, bool)) []gate.Flag {
	if lookup == nil {
		return nil
	}
	rec, ok := lookup(id)
	if !ok || rec == nil {
		return []gate.Flag{{
			Kind:     gate.FlagAttestationUnknown,
			Detail:   fmt.Sprintf("envelope gate-id %q has no record behind it", id),
			Evidence: "jevons: oracle gate-id=" + id,
		}}
	}
	if rec.Verdict.IsKilled() {
		return []gate.Flag{{
			Kind:     gate.FlagAttestationKilled,
			Detail:   fmt.Sprintf("envelope gate-id %q was terminated (verdict KILLED)", id),
			Evidence: rec.Attestation(),
		}}
	}
	if !rec.Verdict.IsGreen() || rec.Status() != "0" {
		return []gate.Flag{{
			Kind:     gate.FlagAttestationNotGreen,
			Detail:   fmt.Sprintf("envelope gate-id %q is verdict %s exit=%s, which is not a pass", id, rec.Verdict, rec.Status()),
			Evidence: rec.Attestation(),
		}}
	}
	return nil
}

// FalseGreenFlagsForReport is FalseGreenFlags plus the one check that needs
// to know whose report this is: every cited gate id is resolved and its
// measured commit asked of the author's own repository (🎯T1027). A gate
// that ran on a commit this repo has never held is another repo's run,
// whatever its verdict, and is flagged foreign_repo_gate. It runs on the
// notify path as well as the reap path — the banner the supervisor reads is
// where the 2026-10-07 misattribution happened. workDir == "" or a workDir
// outside any git work tree skips it: there is no repo to scope to.
func FalseGreenFlagsForReport(report, workDir string) []gate.Flag {
	return append(FalseGreenFlags(report), ForeignGateFlags(report, workDir)...)
}

// ForeignGateFlags is the 🎯T1027 arm alone: cited gates whose record
// measured a commit the repository at workDir does not contain.
func ForeignGateFlags(report, workDir string) []gate.Flag {
	store := gateStore()
	if store == nil {
		return nil
	}
	known, ok := gate.CommitKnownIn(workDir)
	if !ok {
		return nil
	}
	return gate.FlagForeignGates(report, store.Lookup, known)
}

// FalseGreenBanner is the note prepended to a flagged report on its way to
// the overseer, or "" when the report does not contradict itself.
func FalseGreenBanner(report string) string {
	return gate.Banner(FalseGreenFlags(report))
}

// falseGreenLead is the notify-path prefix: a declared fact, then the
// rendered banner, then the report. StripPrefixes peels the fact. The
// rendered banner alone is not a prefix.
func falseGreenLead(agent string, flags []gate.Flag, msg string) string {
	if len(flags) == 0 {
		return msg
	}
	return envelope.PrefixFalseGreen + "\n" + gate.BannerFor(agent, flags) + "\n\n" + msg
}

// falseGreenKinds names the flags for the lifecycle log, which is read as a
// stream rather than as prose.
func falseGreenKinds(flags []gate.Flag) []string {
	kinds := make([]string, 0, len(flags))
	for _, f := range flags {
		kinds = append(kinds, string(f.Kind))
	}
	return kinds
}
