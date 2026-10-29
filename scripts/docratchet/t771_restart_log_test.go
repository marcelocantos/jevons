// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T771: a development restart must append daemon boot lines to the same file
// supervisord captures, not a separate ~/.jevons/daily-jevonsd.log.
func TestT771RestartScriptDefaultsToSupervisorLog(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "scripts", "restart-jevonsd.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, ".local/var/log/jevonsd.log") {
		t.Fatal("restart-jevonsd.sh must default development daemon log to supervisord capture path")
	}
	if strings.Contains(s, `LOG="${JEVONS_RESTART_LOG:-$HOME/.jevons/daily-jevonsd.log}"`) {
		t.Fatal("restart-jevonsd.sh still defaults LOG to daily-jevonsd.log unconditionally")
	}
}
