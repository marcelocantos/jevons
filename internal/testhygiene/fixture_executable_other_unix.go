// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix && !darwin && !linux

package testhygiene

import (
	"fmt"

	"github.com/marcelocantos/jevons/internal/seatload"
)

func fixtureExecutablePaths(_ seatload.Table) (map[int]string, error) {
	return nil, fmt.Errorf("fixture executable census is unsupported on this platform")
}
