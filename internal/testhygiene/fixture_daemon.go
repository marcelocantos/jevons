// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package testhygiene

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatload"
)

// FixtureDaemon is a jevonsd process whose argv shows it was started from a
// go test temp tree (…/T/Test…).
var fixtureDaemonRE = regexp.MustCompile(`/T/Test[^/]+/`)

// CensusFixtureDaemons returns PIDs of jevonsd processes rooted under go test
// temp directories.
func CensusFixtureDaemons(tbl seatload.Table, executables map[int]string) []int {
	var pids []int
	for _, p := range tbl {
		executable := executables[p.PID]
		if filepath.Base(executable) != "jevonsd" {
			continue
		}
		if fixtureDaemonRE.MatchString(executable) {
			pids = append(pids, p.PID)
		}
	}
	return pids
}

// CensusLeakedFixtureDaemons reads the host process table and returns fixture
// jevonsd PIDs that should not outlive their test.
func CensusLeakedFixtureDaemons() ([]int, error) {
	tbl, err := seatload.HostTable()
	if err != nil {
		return nil, err
	}
	executables, err := fixtureExecutablePaths(tbl)
	if err != nil {
		return nil, err
	}
	return CensusFixtureDaemons(tbl, executables), nil
}

// ps comm supplies executable paths, rather than argv text. Split only the PID
// prefix: an executable path can contain spaces and must remain intact.
func parsePSExecutablePaths(output string) (map[int]string, error) {
	result := map[int]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		end := strings.IndexAny(line, " \t")
		if end < 1 {
			return nil, fmt.Errorf("fixture executable census: missing PID/path separator")
		}
		pid, err := strconv.Atoi(line[:end])
		if err != nil || pid <= 0 {
			return nil, fmt.Errorf("fixture executable census: invalid PID")
		}
		path := strings.TrimSpace(line[end:])
		if path == "" {
			return nil, fmt.Errorf("fixture executable census: missing executable path")
		}
		result[pid] = path
	}
	return result, nil
}
