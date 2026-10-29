// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

)

const ledgerPath = "bullseye.yaml"

// mergeTreeKeepingBaseLedger resolves a merge-tree result whose only conflict is
// bullseye.yaml by keeping the shared branch's ledger (🎯T1151.3). Worker
// branches must not carry ledger edits; a stale worktree copy is dropped.
func mergeTreeKeepingBaseLedger(dir, baseTip, workerTip, mergeTreeOID string, conflicts []string) (string, error) {
	if len(conflicts) != 1 || conflicts[0] != ledgerPath {
		return "", fmt.Errorf("not a ledger-only conflict")
	}
	baseLedger, err := gitBlob(dir, baseTip+":"+ledgerPath)
	if err != nil {
		return "", err
	}
	merged := baseLedger
	cmd := exec.Command("git", "-C", dir, "hash-object", "-w", "--stdin")
	cmd.Stdin = bytes.NewReader(merged)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hash-object ledger: %w", err)
	}
	blob := strings.TrimSpace(string(out))
	return replaceTreeBlob(dir, mergeTreeOID, ledgerPath, blob)
}

func replaceTreeBlob(dir, treeOID, path, blobOID string) (string, error) {
	// bullseye.yaml lives at the repo root; mktree accepts one directory level only.
	list, err := gitOut(dir, "ls-tree", "-z", treeOID)
	if err != nil {
		return "", err
	}
	parts := strings.Split(list, "\x00")
	var buf bytes.Buffer
	replaced := false
	for _, ent := range parts {
		if ent == "" {
			continue
		}
		tab := strings.IndexByte(ent, '\t')
		if tab < 0 {
			continue
		}
		sp := strings.Fields(ent[:tab])
		if len(sp) < 3 {
			continue
		}
		mode, typ, oid := sp[0], sp[1], sp[2]
		name := ent[tab+1:]
		if name == path {
			fmt.Fprintf(&buf, "%s %s %s\t%s\x00", mode, typ, blobOID, name)
			replaced = true
			continue
		}
		fmt.Fprintf(&buf, "%s %s %s\t%s\x00", mode, typ, oid, name)
	}
	if !replaced {
		fmt.Fprintf(&buf, "100644 blob %s\t%s\x00", blobOID, path)
	}
	cmd := exec.Command("git", "-C", dir, "mktree", "-z")
	cmd.Stdin = &buf
	treeOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mktree: %w", err)
	}
	return strings.TrimSpace(string(treeOut)), nil
}
