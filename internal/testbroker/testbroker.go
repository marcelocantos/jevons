// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package testbroker keeps hermetic tests off the owner's Claudia broker
// (🎯T975). A test that launched a seat through a registry not in direct mode
// granted a real seat on the development broker: on 2026-10-01 one minted
// jv-t957-real on xAI Grok with AutoStart on every run, and the broker
// resumed it on each restart, spending quota the owner had asked be left
// alone.
package testbroker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeadEnd points Claudia's broker and sidecar sockets at paths nothing
// serves, so a stray launch fails to dial instead of reaching a live broker.
// A test that runs its own broker sets its own socket with t.Setenv. An
// opt-in live run (any *_LIVE variable set, such as JEVONS_LIVE) is left as
// it is: it means to reach real providers.
func DeadEnd() {
	for _, kv := range os.Environ() {
		name, val, _ := strings.Cut(kv, "=")
		if strings.HasSuffix(name, "_LIVE") && val != "" {
			return
		}
	}
	base := filepath.Join(os.TempDir(), fmt.Sprintf("jevons-test-%d", os.Getpid()))
	os.Setenv("CLAUDIA_BROKER_SOCKET", base+"-no-broker.sock")
	os.Setenv("CLAUDIA_OMP_SOCKET", base+"-no-sidecar.sock")
}
