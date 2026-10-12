// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"hash/maphash"
	"sync"

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
	ledgerSeed  = maphash.MakeSeed()
	ledgerMu    sync.Mutex
	ledgerCache = map[ledgerKey]map[string]assignmentTarget{}
)

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
