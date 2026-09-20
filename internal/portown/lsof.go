// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portown

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Lister returns the TCP LISTEN sockets on port. Tests inject a stub so
// the classifier does not depend on macOS SO_REUSEADDR letting * bind
// beside 127.0.0.1 (Linux often cannot).
type Lister func(port int) ([]Listener, error)

// ListLSOF is the product enumerator: lsof -nP -iTCP:port -sTCP:LISTEN.
func ListLSOF(port int) ([]Listener, error) {
	if port <= 0 {
		return nil, fmt.Errorf("portown: invalid port %d", port)
	}
	bin, err := exec.LookPath("lsof")
	if err != nil {
		bin = "/usr/sbin/lsof"
	}
	cmd := exec.Command(bin, "-nP",
		"-iTCP:"+strconv.Itoa(port),
		"-sTCP:LISTEN",
		"-Fpcn")
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && len(out) == 0 {
			// lsof exits 1 when nothing matches.
			return nil, nil
		}
		if len(out) == 0 {
			return nil, err
		}
		// Partial records with a non-zero exit still parse.
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
		case 'n':
			cur.Addr = val
			have = true
		}
	}
	flush()
	return all
}
