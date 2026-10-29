// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package reportcollapse

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

func workerReport(worker, body string) string {
	return envelope.PrefixAgentResponded + "\n" + envelope.SeatFact(envelope.IntentRoute, worker) + "\n" + body
}

func TestPendingSupersededByNonTerminalStacksToOne(t *testing.T) {
	w := "jv-t826-worker"
	old := workerReport(w, "still waiting on gate")
	in := workerReport(w, "still waiting on monitor")
	if !PendingSupersededBy(in, old) {
		t.Fatal("newer non-terminal should supersede older from same worker")
	}
}

func TestPendingSupersededByTerminalBetweenNonTerminalKeepsTerminal(t *testing.T) {
	w := "jv-t826-worker"
	finish := "```jevons\njevons: kind finish-report\njevons: target T826\njevons: oracle sha=abcdef0123456\njevons: silent-ledger none\n```\n"
	term := workerReport(w, finish)
	ping := workerReport(w, "```jevons\njevons: kind status-ping\njevons: status in-progress\n```\n")
	if PendingSupersededBy(ping, term) {
		t.Fatal("non-terminal must not supersede terminal")
	}
	if !PendingSupersededBy(term, ping) {
		t.Fatal("terminal should supersede pending non-terminal progress")
	}
}

func TestPendingSupersededByGateEvidenceIsKept(t *testing.T) {
	w := "jv-t826-worker"
	withGate := workerReport(w, "```jevons\njevons: kind status-ping\njevons: status in-progress\njevons: gate-id=deadbeef\n```\n")
	if !NeverDrop(withGate) {
		t.Fatal("gate-id must make the report protected")
	}
	plain := workerReport(w, "waiting")
	if PendingSupersededBy(plain, withGate) {
		t.Fatal("must not drop report carrying gate-id")
	}
}
