// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ViaBullseye marks content journalled from a sanctioned bullseye tool write
// (MCP or CLI track/apply/achieve). 🎯T1247.
const ViaBullseye = "bullseye"

// RecordLedgerWrite journals absPath as a sanctioned bullseye-tool write so
// the next sweep does not report the replacement as an unguarded loss.
// Best-effort: a journal failure must not fail the ledger mutation.
func RecordLedgerWrite(absPath string) error {
	return recordLedgerWrite(&Store{Root: DefaultStoreRoot()}, absPath, time.Now())
}

func recordLedgerWrite(store *Store, absPath string, now time.Time) error {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	return store.Journal(absPath, content, now, "", ViaBullseye)
}

// sanctionedCommitSlack is clock/granularity slack when comparing a bullseye
// commit's committer date to the journal's last-seen time. A wipe-to-HEAD of
// uncommitted rows journals those rows well after the last bullseye commit
// (minutes in the 🎯T1147 specimen), so one second does not swallow that loss.
const sanctionedCommitSlack = time.Second

// isSanctionedBullseyeWrite reports whether disk is the blob at HEAD:rel and
// the commit that last touched rel is a bullseye tool commit newer than the
// journal. That is the MCP/CLI commit-mode shape (git commit-tree +
// update-ref, then checkout): the write never passed PreToolUse, so the
// journal still holds the previous ledger, but the writer is bullseye.
//
// A stale full-file write or a raw sed does not match HEAD, so it still
// alarms. A checkout of HEAD that drops uncommitted rows journals those rows
// *after* the last bullseye commit, so the recency check fails and the loss
// is still reported.
func isSanctionedBullseyeWrite(repoRoot, rel string, disk []byte, journalAt time.Time) bool {
	if repoRoot == "" || rel == "" {
		return false
	}
	head, err := exec.Command("git", "-C", repoRoot, "show", "HEAD:"+rel).Output()
	if err != nil || !bytes.Equal(head, disk) {
		return false
	}
	subj, err := exec.Command("git", "-C", repoRoot, "log", "-1", "--format=%s", "--", rel).Output()
	if err != nil {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(string(subj)), "bullseye:") {
		return false
	}
	dateRaw, err := exec.Command("git", "-C", repoRoot, "log", "-1", "--format=%cI", "--", rel).Output()
	if err != nil {
		return false
	}
	committed, err := parseGitISODate(strings.TrimSpace(string(dateRaw)))
	if err != nil {
		return false
	}
	return !committed.Before(journalAt.Add(-sanctionedCommitSlack))
}

func parseGitISODate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}
