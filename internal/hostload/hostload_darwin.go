// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package hostload

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// sysctlBin is addressed absolutely: the daemon can be started by launchd on a
// bare PATH (the 🎯T434 lesson), and /usr/sbin is not on it.
const sysctlBin = "/usr/sbin/sysctl"

// readPlatform reads load and swap from the darwin kernel.
//
// It shells out rather than calling sysctl(3) because vm.loadavg and
// vm.swapusage return C structs, and Go's syscall.Sysctl truncates its result
// at the first NUL byte — which is inside both structs. A pipe to /usr/sbin/
// sysctl is cheap at the cadence Cached imposes and, unlike a hand-rolled
// struct decode, cannot silently return a plausible wrong number.
func readPlatform() Sample {
	s := Sample{Source: "darwin sysctl", At: time.Now()}

	out, err := exec.Command(sysctlBin, "-n", "vm.loadavg").Output()
	if err != nil {
		s.Err = "vm.loadavg: " + err.Error()
	} else if l1, l5, l15, ok := parseLoadAvgSysctl(string(out)); ok {
		s.Load1, s.Load5, s.Load15 = l1, l5, l15
	} else {
		s.Err = "vm.loadavg: unparsable " + strings.TrimSpace(string(out))
	}

	out, err = exec.Command(sysctlBin, "-n", "vm.swapusage").Output()
	if err != nil {
		s.Err = strings.TrimSpace(s.Err + " vm.swapusage: " + err.Error())
	} else if used, total, ok := parseSwapUsage(string(out)); ok {
		s.SwapUsedBytes, s.SwapTotalBytes = used, total
	} else {
		s.Err = strings.TrimSpace(s.Err + " vm.swapusage: unparsable " + strings.TrimSpace(string(out)))
	}
	return s
}

// parseLoadAvgSysctl reads "{ 55.29 48.63 43.29 }" as produced by
// `sysctl -n vm.loadavg`.
func parseLoadAvgSysctl(s string) (l1, l5, l15 float64, ok bool) {
	fields := strings.Fields(strings.NewReplacer("{", " ", "}", " ").Replace(s))
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	vals := make([]float64, 3)
	for i := range vals {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || v < 0 {
			return 0, 0, 0, false
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], true
}

// parseSwapUsage reads
// "total = 37888.00M  used = 36300.75M  free = 1587.25M  (encrypted)" as
// produced by `sysctl -n vm.swapusage`. The suffix is a binary unit
// (M = MiB), which is what the kernel means and what Activity Monitor shows.
func parseSwapUsage(s string) (used, total int64, ok bool) {
	var haveUsed, haveTotal bool
	fields := strings.Fields(s)
	for i, f := range fields {
		if i+2 >= len(fields) || fields[i+1] != "=" {
			continue
		}
		n, err := parseSwapQuantity(fields[i+2])
		if err != nil {
			continue
		}
		switch strings.ToLower(f) {
		case "total":
			total, haveTotal = n, true
		case "used":
			used, haveUsed = n, true
		}
	}
	return used, total, haveUsed && haveTotal
}

// parseSwapQuantity converts "36300.75M" to bytes. An unsuffixed number is
// taken as bytes.
func parseSwapQuantity(tok string) (int64, error) {
	mult := int64(1)
	switch {
	case strings.HasSuffix(tok, "K"), strings.HasSuffix(tok, "k"):
		mult = 1 << 10
	case strings.HasSuffix(tok, "M"), strings.HasSuffix(tok, "m"):
		mult = 1 << 20
	case strings.HasSuffix(tok, "G"), strings.HasSuffix(tok, "g"):
		mult = 1 << 30
	case strings.HasSuffix(tok, "T"), strings.HasSuffix(tok, "t"):
		mult = 1 << 40
	}
	if mult > 1 {
		tok = tok[:len(tok)-1]
	}
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, err
	}
	return int64(v * float64(mult)), nil
}
