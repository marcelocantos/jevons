// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package proclist reads the host process table in-process.
//
// Darwin uses sysctl (KERN_PROC_ALL plus KERN_PROCARGS2) so a
// workspace-write seatbelt that cannot exec setuid /bin/ps still sees
// pid, ppid, pgid and args (🎯T1232). Other Unix platforms keep the
// previous ps-based listing.
package proclist

import "time"

// Proc is one live process. Command is the full argv, space-joined the
// way `ps -o args=` prints it. Path is the executable path when known.
type Proc struct {
	PID, PPID, PGID int
	CPUPercent      float64
	Elapsed         time.Duration
	Command         string
	Path            string
}
