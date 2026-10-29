// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"hash/maphash"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Parsed-ledger cache (🎯T1067).
//
// The status and owner lookups each parsed the whole ledger, and the daemon
// asks them once per seat on every fleet pass and impatience tick. With a
// 4 MB ledger and ~80 seats that was about 15 GB of YAML allocation in four
// minutes, which pushed the daemon's RSS to several GB in bursts. A ledger
// only changes when its bytes do, so one parse per distinct content serves
// every lookup until it is edited.

// ledgerCacheEntries bounds how many distinct ledgers stay parsed: one per
// repository a fleet works in, plus a just-superseded version.
const ledgerCacheEntries = 16

var (
	ledgerSeed           = maphash.MakeSeed()
	ledgerMu             sync.Mutex
	ledgerCache          = map[ledgerKey]map[string]assignmentTarget{}
	frontierLedgerCache  = map[ledgerKey]frontierLedgerDoc{}
	ledgerReadOps        uint64
	ledgerParseOps       uint64
)

// ResetLedgerIOStats clears read/parse counters (🎯T1152 tests).
func ResetLedgerIOStats() {
	atomic.StoreUint64(&ledgerReadOps, 0)
	atomic.StoreUint64(&ledgerParseOps, 0)
}

// LedgerReadOps returns recorded ledger ReadFile calls.
func LedgerReadOps() uint64 { return atomic.LoadUint64(&ledgerReadOps) }

// LedgerParseOps returns recorded full-document ledger YAML parses.
func LedgerParseOps() uint64 { return atomic.LoadUint64(&ledgerParseOps) }

func recordLedgerRead() { atomic.AddUint64(&ledgerReadOps, 1) }

func recordLedgerParse() { atomic.AddUint64(&ledgerParseOps, 1) }

type ledgerKey struct {
	sum uint64
	n   int
}

// parsedTargets returns the targets of a ledger document, parsing it at most
// once per distinct content. The map is shared: callers must not modify it.
// ok is false when the document does not parse or has no targets.
func parsedTargets(data []byte) (map[string]assignmentTarget, bool) {
	key := ledgerKey{sum: maphash.Bytes(ledgerSeed, data), n: len(data)}
	ledgerMu.Lock()
	rows, hit := ledgerCache[key]
	ledgerMu.Unlock()
	if hit {
		return rows, rows != nil
	}
	var doc assignmentDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		doc.Targets = nil
	}
	ledgerMu.Lock()
	if len(ledgerCache) >= ledgerCacheEntries {
		for k := range ledgerCache {
			delete(ledgerCache, k)
			break
		}
	}
	ledgerCache[key] = doc.Targets
	ledgerMu.Unlock()
	return doc.Targets, doc.Targets != nil
}

// parsedFrontierDoc returns a parsed frontier ledger, at most once per distinct
// content (🎯T1067 / 🎯T1152).
func parsedFrontierDoc(data []byte) (frontierLedgerDoc, bool) {
	key := ledgerKey{sum: maphash.Bytes(ledgerSeed, data), n: len(data)}
	ledgerMu.Lock()
	doc, hit := frontierLedgerCache[key]
	ledgerMu.Unlock()
	if hit {
		return doc, doc.Targets != nil
	}
	var parsed frontierLedgerDoc
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		parsed.Targets = nil
	} else {
		recordLedgerParse()
	}
	ledgerMu.Lock()
	if len(frontierLedgerCache) >= ledgerCacheEntries {
		for k := range frontierLedgerCache {
			delete(frontierLedgerCache, k)
			break
		}
	}
	frontierLedgerCache[key] = parsed
	ledgerMu.Unlock()
	return parsed, parsed.Targets != nil
}
