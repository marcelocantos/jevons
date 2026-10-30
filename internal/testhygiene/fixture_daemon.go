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

// FixtureDaemon is a jevonsd process whose executable shows it was started
// from a go test temp tree (…/T/Test…). Arguments alone are not evidence.
var fixtureDaemonRE = regexp.MustCompile(`/T/Test[^/]+/`)

// CensusFixtureDaemons returns PIDs of jevonsd processes rooted under go test
// temp directories, including fixtures of tests that are still running.
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

// LeakedFixtureDaemons excludes fixtures of still-running tests. Attached
// fixtures have the test binary in their parent chain. Restart-script fixtures
// detach: their testreap watcher names the same test temp root, and the
// watcher is itself a child of the test binary until that binary exits.
func LeakedFixtureDaemons(tbl seatload.Table, executables map[int]string) []int {
	byPID := make(map[int]seatload.Proc, len(tbl))
	for _, p := range tbl {
		byPID[p.PID] = p
	}
	var leaked []int
	for _, pid := range CensusFixtureDaemons(tbl, executables) {
		path := executables[pid]
		match := fixtureDaemonRE.FindStringIndex(path)
		root := path[:match[1]] // includes /T/Test…/; excludes neighbouring tests
		if hasLiveTestAncestor(byPID[pid], byPID, executables) {
			continue
		}
		active := false
		for _, watcher := range tbl {
			if !strings.Contains(watcher.Command, " testreap ") || !strings.Contains(watcher.Command, root) {
				continue
			}
			// A watcher can remain alive briefly after its test has died.
			if _, ok := byPID[watcher.PPID]; ok && isTestBinary(watcher.PPID, executables) {
				active = true
				break
			}
		}
		if !active {
			leaked = append(leaked, pid)
		}
	}
	return leaked
}

func isTestBinary(pid int, executables map[int]string) bool {
	return strings.HasSuffix(filepath.Base(executables[pid]), ".test")
}

func hasLiveTestAncestor(p seatload.Proc, byPID map[int]seatload.Proc, executables map[int]string) bool {
	seen := map[int]bool{p.PID: true}
	for p.PPID > 1 && !seen[p.PPID] {
		parent, ok := byPID[p.PPID]
		if !ok {
			break
		}
		if isTestBinary(parent.PID, executables) {
			return true
		}
		seen[parent.PID] = true
		p = parent
	}
	return false
}

// CensusLeakedFixtureDaemons reads the host process table and reports only
// fixture daemons whose originating test is no longer running.
func CensusLeakedFixtureDaemons() ([]int, error) {
	tbl, err := seatload.HostTable()
	if err != nil {
		return nil, err
	}
	executables, err := fixtureExecutablePaths(tbl)
	if err != nil {
		return nil, err
	}
	return LeakedFixtureDaemons(tbl, executables), nil
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
