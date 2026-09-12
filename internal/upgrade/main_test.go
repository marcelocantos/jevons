// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"os"
	"testing"
)

// TestMain keeps these hermetics off a claudia daemon installed on the
// machine: with one reachable, claudia.BrokerAvailable is true and every
// Registry launch would be granted a real seat. Tests that model the
// daemon stub the brokerAvailable seam instead.
func TestMain(m *testing.M) {
	if os.Getenv("CLAUDIA_NO_BROKER") == "" {
		_ = os.Setenv("CLAUDIA_NO_BROKER", "1")
	}
	os.Exit(m.Run())
}
