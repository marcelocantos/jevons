// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"os/exec"
	"strings"
	"unicode"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// fillIdleResidueEvidence attaches 🎯T410 daemon-held fields to an idle
// open-mission AgentObs: bound-target commits (git log --grep), ledger
// status / owned_by, report-store finish vs ask, and blocked_owner intent.
// Phase=idle alone must not decide stalled vs finished vs blocked.
func fillIdleResidueEvidence(ao *staffops.AgentObs, d claudia.AgentDef, stateDir string, intent fleetintent.Snapshot) {
	if ao == nil {
		return
	}
	target := strings.TrimSpace(d.TargetID)
	ao.BoundTarget = target
	workdir := strings.TrimSpace(d.WorkDir)

	if target != "" && workdir != "" {
		ao.HasBoundCommits = hasBoundTargetCommits(workdir, target)
		if st, ok := targetfile.LoadTargetStatusFromCwd(workdir, target); ok {
			ao.TargetLedgerStatus = st
		}
		if targetfile.IsOwnerAssigned(workdir, target) {
			ao.OwnerAskPresent = true
		}
	}

	if strings.TrimSpace(stateDir) != "" && strings.TrimSpace(d.Name) != "" {
		if rec, err := agentreport.Latest(stateDir, d.Name); err == nil && strings.TrimSpace(rec.Text) != "" {
			ao.ReportLooksFinished = LooksLikeFinishedWorkReport(rec.Text)
			if ReportAwaitsOverseer(rec.Text) {
				ao.OwnerAskPresent = true
			}
		}
	}

	if intent.AgentState(d.Name) == fleetintent.BlockedOwner {
		ao.IntentBlockedOwner = true
		ao.OwnerAskPresent = true
	}
}

// hasBoundTargetCommits is true when git log --grep finds a commit mentioning
// the bound target id (🎯T410 evidence). Refuses empty / option-shaped ids.
func hasBoundTargetCommits(workdir, targetID string) bool {
	id := normalizeTargetGrepID(targetID)
	if id == "" || strings.TrimSpace(workdir) == "" {
		return false
	}
	patterns := []string{"🎯" + id, id}
	seen := map[string]bool{}
	for _, p := range patterns {
		if seen[p] {
			continue
		}
		seen[p] = true
		cmd := exec.Command("git", "-C", workdir, "log", "--grep="+p, "--oneline", "-n", "1")
		out, err := cmd.Output()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			return true
		}
	}
	return false
}

// normalizeTargetGrepID strips the 🎯 prefix and refuses strings that could
// be parsed as git options or that carry control characters.
func normalizeTargetGrepID(targetID string) string {
	id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(targetID), "🎯"))
	if id == "" || strings.HasPrefix(id, "-") {
		return ""
	}
	for _, r := range id {
		if r < 32 || r == 127 || !unicode.IsPrint(r) {
			return ""
		}
		switch r {
		case '/', '\\', '\n', '\r', '\x00':
			return ""
		}
	}
	return id
}
