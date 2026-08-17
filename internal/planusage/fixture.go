// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"os"
	"strings"
)

// FixtureEnv is the isolate hook: when set, Snapshot reads this JSON
// file instead of the last claudia fetch (🎯T390.1.5 journeys).
const FixtureEnv = "JEVONS_PLAN_USAGE_FIXTURE"

// LoadSnapshotFile reads a Snapshot JSON document from path.
func LoadSnapshotFile(path string) (Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func fixturePath() string {
	return strings.TrimSpace(os.Getenv(FixtureEnv))
}
