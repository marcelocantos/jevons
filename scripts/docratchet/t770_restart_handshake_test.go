// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT770SupervisedUpgradeHandshakeWaitsForRelaunch(t *testing.T) {
	script := readRepo(t, "scripts/restart-jevonsd.sh")
	if strings.Contains(script, "supervised daemon did not finish upgrade exit") {
		t.Fatal("restart-jevonsd still dies on old-pid exit wait instead of relaunch success (🎯T770)")
	}
	if !strings.Contains(script, "ensure_supervised_relaunch \"$managed_pid\" \"$policy\" \"$((STOP_WAIT_SEC + RELAUNCH_WAIT_SEC))\"") {
		t.Fatal("supervised upgrade path must budget STOP_WAIT_SEC + RELAUNCH_WAIT_SEC for the handshake (🎯T770)")
	}
}
