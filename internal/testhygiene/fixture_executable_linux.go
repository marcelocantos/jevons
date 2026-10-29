// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package testhygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatload"
)

func fixtureExecutablePaths(table seatload.Table) (map[int]string, error) {
	paths := map[int]string{}
	for _, process := range table {
		// Preserve the previous candidate set without inspecting unrelated users'
		// executables. Arguments only select metadata reads; /proc decides identity.
		if !strings.Contains(process.Command, "jevonsd") || !fixtureDaemonRE.MatchString(process.Command) {
			continue
		}
		path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(process.PID), "exe"))
		if os.IsNotExist(err) {
			continue
		} // The process ended between snapshots.
		if err != nil {
			return nil, fmt.Errorf("fixture executable PID %d: %w", process.PID, err)
		}
		paths[process.PID] = strings.TrimSuffix(path, " (deleted)")
	}
	return paths, nil
}
