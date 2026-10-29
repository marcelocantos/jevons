// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// The ledger shape bullseye wrote in a real J51 run (2026-10-29): names
// unquoted, worker 1 filed three times with the spares set aside. The old
// matcher looked only for quoted names and never saw a pass.
const t1151LedgerSpecimen = `schema_version: 5
targets:
  T1287:
    name: T1151 J51 worker 1
    status: achieved
  T1288:
    name: T1151 J51 worker 1
    status: set_aside
  T1291:
    name: T1151 J51 worker 0
    status: achieved
  T1292:
    name: 'T1151 J51 worker 3'
    status: identified
`

func TestT1151TrackAchievedReadsUnquotedAndDuplicateRows(t *testing.T) {
	for name, want := range map[string]bool{
		"T1151 J51 worker 0": true,
		"T1151 J51 worker 1": true,
		"T1151 J51 worker 3": false,
		"T1151 J51 worker 2": false,
	} {
		if got := t1151TrackAchieved(t1151LedgerSpecimen, name); got != want {
			t.Errorf("t1151TrackAchieved(%q) = %v, want %v", name, got, want)
		}
	}
}
