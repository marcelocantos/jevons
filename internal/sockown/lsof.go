// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sockown

import (
	"os/exec"
	"strconv"
	"strings"
)

// ListLSOF is the product enumerator: lsof on a Unix socket path.
func ListLSOF(socket string) ([]Listener, error) {
	if strings.TrimSpace(socket) == "" {
		return nil, nil
	}
	bin, err := exec.LookPath("lsof")
	if err != nil {
		bin = "/usr/sbin/lsof"
	}
	cmd := exec.Command(bin, "-nP", "-Fpc", "--", socket)
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && len(out) == 0 {
			return nil, nil
		}
		if len(out) == 0 {
			return nil, err
		}
	}
	return parseLSOF(string(out)), nil
}

func parseLSOF(out string) []Listener {
	var (
		cur  Listener
		have bool
		all  []Listener
	)
	flush := func() {
		if !have || cur.PID == 0 {
			return
		}
		all = append(all, cur)
		cur = Listener{}
		have = false
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		tag, val := line[0], line[1:]
		switch tag {
		case 'p':
			flush()
			n, err := strconv.Atoi(val)
			if err != nil {
				continue
			}
			cur.PID = n
			have = true
		case 'c':
			cur.Cmd = val
			have = true
		}
	}
	flush()
	return all
}
