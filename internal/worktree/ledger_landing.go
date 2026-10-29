// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"fmt"
	"os/exec"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

// ErrLedgerShrink means the landing would move the shared branch to a
// bullseye.yaml that drops too many target rows versus the current tip
// (🎯T1144).
var ErrLedgerShrink = fmt.Errorf("landing would shrink bullseye.yaml")

func refuseLandingLedgerShrink(dir, baseTip, landingTip string) error {
	head, err := gitBlob(dir, baseTip+":bullseye.yaml")
	if err != nil {
		return nil // no ledger at tip — nothing to guard
	}
	landed, err := gitBlob(dir, landingTip+":bullseye.yaml")
	if err != nil {
		return nil
	}
	refuse, msg := commitscope.LedgerShrinkLandingRefusal(head, landed)
	if !refuse {
		return nil
	}
	return fmt.Errorf("integrate: %s\n%w", msg, ErrLedgerShrink)
}

func gitBlob(dir, revPath string) ([]byte, error) {
	cmd := exec.Command("git", "-C", dir, "show", revPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}
