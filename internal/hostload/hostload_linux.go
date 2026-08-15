// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package hostload

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// readPlatform reads load and swap from procfs. The daemon's home is a Mac,
// but CI and any future Linux host must not silently lose the lever that
// 🎯T463 exists to add — a missing reading is reported, not assumed healthy.
func readPlatform() Sample {
	s := Sample{Source: "linux procfs", At: time.Now()}

	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		s.Err = "/proc/loadavg: " + err.Error()
	} else if l1, l5, l15, ok := parseProcLoadAvg(string(data)); ok {
		s.Load1, s.Load5, s.Load15 = l1, l5, l15
	} else {
		s.Err = "/proc/loadavg: unparsable " + strings.TrimSpace(string(data))
	}

	data, err = os.ReadFile("/proc/meminfo")
	if err != nil {
		s.Err = strings.TrimSpace(s.Err + " /proc/meminfo: " + err.Error())
	} else if used, total, ok := parseProcMeminfoSwap(string(data)); ok {
		s.SwapUsedBytes, s.SwapTotalBytes = used, total
	}
	return s
}

// parseProcLoadAvg reads "0.42 0.31 0.28 1/512 12345".
func parseProcLoadAvg(s string) (l1, l5, l15 float64, ok bool) {
	fields := strings.Fields(s)
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

// parseProcMeminfoSwap derives used swap from SwapTotal and SwapFree, both
// reported in kB. A host with no swap reports SwapTotal 0, which stays
// "unknown" rather than becoming a full swap.
func parseProcMeminfoSwap(s string) (used, total int64, ok bool) {
	var totalKB, freeKB int64
	var haveTotal, haveFree bool
	for _, line := range strings.Split(s, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "SwapTotal":
			totalKB, haveTotal = n, true
		case "SwapFree":
			freeKB, haveFree = n, true
		}
	}
	if !haveTotal || !haveFree || totalKB <= 0 {
		return 0, 0, false
	}
	if freeKB > totalKB {
		freeKB = totalKB
	}
	return (totalKB - freeKB) * 1024, totalKB * 1024, true
}
