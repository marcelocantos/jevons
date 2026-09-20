// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package seatload

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// psArgs reads the whole process table in one shot. The keywords are the
// ones both BSD (darwin) and GNU (linux) ps agree on: etimes is not
// portable, so elapsed time is parsed from etime's [[dd-]hh:]mm:ss.
var psArgs = []string{"-Ao", "pid=,ppid=,pgid=,pcpu=,etime=,args="}

// HostTable reads the host process table via ps.
func HostTable() (Table, error) {
	out, err := exec.Command("ps", psArgs...).Output()
	if err != nil {
		return nil, fmt.Errorf("seatload: ps: %w", err)
	}
	return ParsePS(string(out)), nil
}

// ParsePS parses `ps -Ao pid=,ppid=,pgid=,pcpu=,etime=,args=` output. A row
// that does not parse is dropped rather than guessed at: a fabricated row on
// this path would point the reaper at a process nobody asked about.
func ParsePS(out string) Table {
	var t Table
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[3], 64)
		t = append(t, Proc{
			PID:        pid,
			PPID:       ppid,
			PGID:       pgid,
			CPUPercent: cpu,
			Elapsed:    ParseETime(fields[4]),
			Command:    strings.Join(fields[5:], " "),
		})
	}
	return t
}

// ParseETime parses ps's [[dd-]hh:]mm:ss elapsed-time format. An
// unparseable value is zero, which reads as "unknown age", never as a
// fabricated one.
func ParseETime(s string) time.Duration {
	days := 0
	if i := strings.Index(s, "-"); i >= 0 {
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0
		}
		days, s = d, s[i+1:]
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0
		}
		nums[i] = n
	}
	var h, m, sec int
	if len(nums) == 3 {
		h, m, sec = nums[0], nums[1], nums[2]
	} else {
		m, sec = nums[0], nums[1]
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute +
		time.Duration(sec)*time.Second
}

func hostPID() int { return os.Getpid() }
