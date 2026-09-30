// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
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
// the flags in front of it. Auto-reap is not (🎯T470): a report carrying any
// false-green flag is never read as finished_work in the same pass — the
// daemon must not hold both "evidence does not support the claim" and "the
// claim is true". Overseer judgment of the annotated report remains separate.

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
	body := report
	var flags []gate.Flag
	if m, err := envelope.Parse(report); m != nil {
		if m.Payload != "" {
			body = m.Payload
		}
		if err == nil && m.GateID != "" && m.Verdict.IsPass() {
			flags = append(flags, envelopeGateFlags(m.GateID, lookup)...)
		}
	}
	return append(flags, gate.FlagFalseGreen(body, lookup)...)
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

// FalseGreenFlagsForWorker is FalseGreenFlags plus the one check only the
// reap path can make: it alone knows which directory the worker occupies
// (🎯T946). A report that reads as oracle evidence on marker words alone —
// no cited SHA, no cited gate id — while that worktree carries uncommitted
// tracked changes or new untracked source files is flagged the same way a
// contradicted attestation is: nothing was landed for "pass"/"green" to
// describe. workDir == "" (a def the fixture never gave one, or a probe that
// found no git repo) skips the check and this degrades to FalseGreenFlags.
func FalseGreenFlagsForWorker(report, workDir string) []gate.Flag {
	flags := FalseGreenFlags(report)
	if strings.TrimSpace(workDir) == "" {
		return flags
	}
	tree := gate.ProbeTree(workDir)
	if !UncitedClaimAgainstDirtyWorktree(report, tree) {
		return flags
	}
	return append(flags, gate.Flag{
		Kind: gate.FlagUncitedClaimDirtyWorktree,
		Detail: fmt.Sprintf(
			"the report reads as oracle evidence on marker words alone (no SHA, no gate id) while %s carries %d uncommitted change(s) — nothing was landed for the claim to describe",
			workDir, tree.DirtyFiles),
		Evidence: strings.Join(tree.DirtySample, ", "),
	})
}

// FalseGreenBanner is the note prepended to a flagged report on its way to
// the overseer, or "" when the report does not contradict itself.
func FalseGreenBanner(report string) string {
	return gate.Banner(FalseGreenFlags(report))
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
