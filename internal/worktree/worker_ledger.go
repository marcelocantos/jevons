// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"fmt"
	"strings"
)

// ErrWorkerLedger means the worker branch changed bullseye.yaml. Ledger
// effects go through the atomic writer on the shared branch (🎯T1151.3).
var ErrWorkerLedger = fmt.Errorf("worker branch changes bullseye.yaml")

// refuseWorkerBranchLedgerChanges rejects a landing when any commit on the
// worker branch (since its merge base with the shared tip) touches the ledger.
func refuseWorkerBranchLedgerChanges(dir, baseTip, workerTip string) error {
	mb, err := gitOut(dir, "merge-base", baseTip, workerTip)
	if err != nil {
		return fmt.Errorf("integrate: merge-base: %w", err)
	}
	diff, err := gitOut(dir, "diff", "--name-only", mb+".."+workerTip, "--", ledgerPath)
	if err != nil {
		return fmt.Errorf("integrate: diff ledger on worker branch: %w", err)
	}
	if diff == "" {
		return nil
	}
	log, err := gitOut(dir, "log", "--oneline", mb+".."+workerTip, "--", ledgerPath)
	if err != nil {
		return fmt.Errorf("integrate: log ledger commits: %w", err)
	}
	commits := strings.TrimSpace(log)
	if commits == "" {
		commits = "(commits touch bullseye.yaml)"
	}
	return fmt.Errorf("integrate: worker branch must not change %s; offending commits:\n%s\n%w",
		ledgerPath, commits, ErrWorkerLedger)
}

// workerTipLedgerStale reports whether the worker tip still carries an older
// ledger blob than the shared tip even though no worker commit edited it.
func workerTipLedgerStale(dir, baseTip, workerTip string) bool {
	baseLed, err1 := gitBlob(dir, baseTip+":"+ledgerPath)
	workerLed, err2 := gitBlob(dir, workerTip+":"+ledgerPath)
	if err1 != nil || err2 != nil {
		return false
	}
	return !bytesEqual(baseLed, workerLed)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
