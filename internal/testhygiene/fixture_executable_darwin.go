// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package testhygiene

import (
	"fmt"

	"github.com/marcelocantos/jevons/internal/proclist"
	"github.com/marcelocantos/jevons/internal/seatload"
)

func fixtureExecutablePaths(_ seatload.Table) (map[int]string, error) {
	procs, err := proclist.All()
	if err != nil {
		return nil, fmt.Errorf("fixture executable census: %w", err)
	}
	result := map[int]string{}
	for _, p := range procs {
		if p.PID <= 0 || p.Path == "" {
			continue
		}
		result[p.PID] = p.Path
	}
	return result, nil
}
