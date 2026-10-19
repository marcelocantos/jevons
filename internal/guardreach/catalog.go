// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package guardreach is 🎯T419: a guard against a silent failure is attested
// by a test that reaches it through its production call site, under the
// predicate that gates that caller — not by invoking the guarded function
// alone.
//
// WHY. On 2026-08-10 handover.Store.MarkDelivered had a green unit test
// (TestStoreMarkDeliveredPreventsDoubleSeeding) and was never reached in
// production: migrate.go called it only when Claudia.Deliver returned nil,
// and Deliver waited for reply completion inside a 10-minute timeout, not
// turn-begin. A delivered seed stayed pending; the double-seed guard never
// fired. The unit test attested the function, not the behaviour.
//
// The handover path is gone (in-place switch, claudia 🎯T195). The property
// is not. This package holds the standing catalogue of silent-failure
// guards, their production callers, and the tests that must drive them
// through those callers. scripts/docratchet and package tests enforce the
// catalogue; an audit file reports residual candidates rather than
// silently "fixing" them into the catalogue without judgment.
package guardreach

// Status of a catalogue entry.
const (
	// StatusEnforced: production call graph and call-site test are checked.
	StatusEnforced = "enforced"
	// StatusRetired: the guard (and its call site) no longer exist; the
	// catalogue pins the absence so a silent reintroduction fails.
	StatusRetired = "retired"
	// StatusAuditOnly: found by audit, reported, not yet promoted to a
	// hard reachability check. Clause 3 of 🎯T419: report rather than
	// silently fix.
	StatusAuditOnly = "audit-only"
)

// Entry is one silent-failure guard and the production path that must
// reach it.
type Entry struct {
	// ID is a stable short name used in reports and tests.
	ID string
	// Guard is the guarded symbol (function or method base name) whose
	// failure is silent when the production caller never reaches it.
	Guard string
	// Intermediate is the production helper that is the real call site of
	// Guard (may equal Guard when the production function is the guard).
	Intermediate string
	// ProductionFiles are non-test source paths (repo-relative) that must
	// name Intermediate. Empty for StatusRetired.
	ProductionFiles []string
	// CallSiteTest is a substring that must appear as a test name (or in
	// a test file) proving the production path under the gating predicate.
	// Empty for StatusRetired / StatusAuditOnly without a pin yet.
	CallSiteTest string
	// Predicate is free-text: the condition that gates the production
	// caller. The call-site test must exercise that condition, not merely
	// invoke Guard.
	Predicate string
	// Status is one of StatusEnforced, StatusRetired, StatusAuditOnly.
	Status string
	// Note carries retirement / audit provenance.
	Note string
}

// Catalog is the standing inventory. Adding a silent-failure guard means
// adding a row here and a call-site test; removing a path means StatusRetired
// with a note, not deleting the memory.
func Catalog() []Entry {
	return []Entry{
		{
			ID:           "handover-mark-delivered",
			Guard:        "MarkDelivered",
			Intermediate: "MarkDelivered",
			Status:       StatusRetired,
			Predicate:    "Claudia.Deliver returned nil (reply-completion inside defaultReplyTimeout)",
			Note: "Pinning instance of 🎯T419 (2026-08-10). internal/handover and " +
				"fleet.SeedSuccessor were removed when seats switched provider/model in place " +
				"(claudia 🎯T195, commit 2815211c). The living successor is delivery-record-on-begun.",
		},
		{
			ID:           "delivery-record-on-begun",
			Guard:        "RecordPayload",
			Intermediate: "recordDeliveryEvidence",
			ProductionFiles: []string{
				"internal/mcpserver/agent_send.go",
				"internal/mcpserver/deliver_by_name.go",
				"internal/mcpserver/event_push.go",
				"internal/mcpserver/notify_replay.go",
				"internal/mcpserver/delivery_evidence.go",
			},
			CallSiteTest: "TestT419DeliveryEvidenceReachedThroughBegunSend",
			Predicate:    "SendOutcome == OutcomeBegun (turn-begin, not reply-completion)",
			Status:       StatusEnforced,
			Note: "Living successor of MarkDelivered. reportSendOutcome records durable " +
				"delivery evidence only on OutcomeBegun (🎯T416/🎯T417). A unit test of " +
				"delivery.RecordPayload alone does not close this entry.",
		},
		{
			ID:           "delivery-claim-exclusive",
			Guard:        "claimRecordFile",
			Intermediate: "Record",
			ProductionFiles: []string{
				"internal/delivery/store.go",
				"internal/mcpserver/delivery_evidence.go",
			},
			CallSiteTest: "TestT749",
			Predicate:    "Record/RecordPayload writes evidence (linkJSONExclusive must not overwrite)",
			Status:       StatusEnforced,
			Note: "🎯T749: same-second agent+needle collisions must not silently drop a record. " +
				"Covered through delivery.Record and mcpserver recordDeliveryEvidence paths.",
		},
		{
			ID:           "sendq-resolve-on-drain",
			Guard:        "Resolve",
			Intermediate: "drainAgentSendQueueOnce",
			ProductionFiles: []string{
				"internal/mcpserver/agent_send.go",
			},
			CallSiteTest: "TestDrained",
			Predicate:    "drain path after a live process attempt (Confirmed / Unverified / DefinitelyNotSent)",
			Status:       StatusEnforced,
			Note: "A queue entry whose attempt is never resolved stalls forever (🎯T418/🎯T726). " +
				"Unit tests of sendq.Resolve alone do not prove the drain reaches it.",
		},
		{
			ID:           "treeguard-ledger-discard",
			Guard:        "decideLedgerDiscard",
			Intermediate: "Pre",
			ProductionFiles: []string{
				"internal/treeguard/hook.go",
			},
			CallSiteTest: "ledger_discard_e2e",
			Predicate:    "PreToolUse Bash path for bare git reset/clean with dirty bullseye.yaml",
			Status:       StatusEnforced,
			Note: "Bare git reset --hard does not name a pathspec; the guard is only reached " +
				"through Env.Pre. Direct unit calls without Pre are not production reachability.",
		},
		// Audit-only residuals (🎯T419 clause 3): reported, not auto-fixed.
		{
			ID:           "audit-sendq-sanitise-path",
			Guard:        "sanitise",
			Intermediate: "sanitise",
			Status:       StatusAuditOnly,
			Predicate:    "path element rewrite on sendq store open",
			Note: "internal/sendq documents the same silent-rename hazard as handover.Store.path. " +
				"Direct store tests cover it; promote to enforced if a production caller is found " +
				"that can skip sanitise under a gating predicate.",
		},
		{
			ID:           "audit-spool-resume-fail-closed",
			Guard:        "ResumeFromSpool",
			Intermediate: "ResumeFromSpool",
			Status:       StatusAuditOnly,
			Predicate:    "seat resume consults spool",
			Note: "Fail-closed resume is load-bearing; confirm a call-site test drives the " +
				"production resume path under the spool-present predicate before enforcing.",
		},
	}
}

// Enforced returns only StatusEnforced entries.
func Enforced() []Entry {
	var out []Entry
	for _, e := range Catalog() {
		if e.Status == StatusEnforced {
			out = append(out, e)
		}
	}
	return out
}

// Retired returns only StatusRetired entries.
func Retired() []Entry {
	var out []Entry
	for _, e := range Catalog() {
		if e.Status == StatusRetired {
			out = append(out, e)
		}
	}
	return out
}

// AuditOnly returns StatusAuditOnly entries (clause 3 report surface).
func AuditOnly() []Entry {
	var out []Entry
	for _, e := range Catalog() {
		if e.Status == StatusAuditOnly {
			out = append(out, e)
		}
	}
	return out
}
