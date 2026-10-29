// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix && !darwin

package proclist

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// All lists every process via ps. Darwin has a sysctl implementation;
// other Unix keeps this path because /bin/ps is not setuid there in the
// way that trips a workspace-write seatbelt.
func All() ([]Proc, error) {
	out, err := exec.Command("ps", "-Ao", "pid=,ppid=,pgid=,pcpu=,etime=,args=").Output()
	if err != nil {
		return nil, fmt.Errorf("proclist: ps: %w", err)
	}
	procs := parsePS(string(out))
	if len(procs) == 0 {
		return nil, fmt.Errorf("proclist: ps listed no processes")
	}
	return procs, nil
}

// Lookup reads one pid. ok is false when that pid is not running.
func Lookup(pid int) (Proc, bool, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,pgid=,pcpu=,etime=,args=").Output()
	if err != nil {
		if strings.TrimSpace(string(out)) == "" {
			return Proc{}, false, nil
		}
		return Proc{}, false, fmt.Errorf("proclist: ps -p %d: %w", pid, err)
	}
	procs := parsePS(string(out))
	if len(procs) == 0 {
		return Proc{}, false, nil
	}
	return procs[0], true, nil
}

func parsePS(out string) []Proc {
	var t []Proc
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
		cmd := strings.Join(fields[5:], " ")
		t = append(t, Proc{
			PID:        pid,
			PPID:       ppid,
			PGID:       pgid,
			CPUPercent: cpu,
			Elapsed:    parseETime(fields[4]),
			Command:    cmd,
			Path:       fields[5],
		})
	}
	return t
}

func parseETime(s string) time.Duration {
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
