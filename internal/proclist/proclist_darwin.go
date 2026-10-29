// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package proclist

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Darwin sys/param.h FSHIFT is 11; FSCALE is 1<<11. p_pctcpu is that
// fixpt_t, and 100*pctcpu/FSCALE is the percent of one core ps prints.
const darwinCPUScale = 2048

// All lists every process via kern.proc.all and KERN_PROCARGS2. It never
// execs /bin/ps.
func All() ([]Proc, error) {
	ks, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("proclist: kern.proc.all: %w", err)
	}
	if len(ks) == 0 {
		return nil, fmt.Errorf("proclist: kern.proc.all returned no processes")
	}
	out := make([]Proc, 0, len(ks))
	for i := range ks {
		out = append(out, fromKinfo(ks[i]))
	}
	return out, nil
}

// Lookup reads one pid. ok is false when that pid is not running.
func Lookup(pid int) (Proc, bool, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		// Missing and out-of-range pids surface as ESRCH, EIO, or EINVAL
		// depending on the kernel; none of those mean "the table is unreadable".
		return Proc{}, false, nil
	}
	return fromKinfo(*k), true, nil
}

func fromKinfo(k unix.KinfoProc) Proc {
	p := Proc{
		PID:        int(k.Proc.P_pid),
		PPID:       int(k.Eproc.Ppid),
		PGID:       int(k.Eproc.Pgid),
		CPUPercent: 100 * float64(k.Proc.P_pctcpu) / darwinCPUScale,
		Elapsed:    elapsedSince(k.Proc.P_starttime),
		Command:    cString(k.Proc.P_comm[:]),
	}
	path, args := procArgs(p.PID)
	if path != "" {
		p.Path = path
	}
	switch {
	case len(args) > 0:
		p.Command = strings.Join(args, " ")
	case path != "":
		p.Command = path
	}
	return p
}

func elapsedSince(tv unix.Timeval) time.Duration {
	start := time.Unix(tv.Sec, int64(tv.Usec)*int64(time.Microsecond))
	d := time.Since(start)
	if d < 0 {
		return 0
	}
	return d
}

func cString(b []byte) string {
	if n := bytes.IndexByte(b, 0); n >= 0 {
		return string(b[:n])
	}
	return string(b)
}

// procArgs parses KERN_PROCARGS2: int32 argc, then the exec path, then
// argc NUL-terminated argv strings. Kernel tasks and processes that
// vanish between the table read and this call return empty.
func procArgs(pid int) (string, []string) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(buf) < 4 {
		return "", nil
	}
	argc := int(int32(binary.LittleEndian.Uint32(buf[:4])))
	if argc < 0 || argc > 4096 {
		argc = 0
	}
	rest := buf[4:]
	n := bytes.IndexByte(rest, 0)
	if n < 0 {
		return "", nil
	}
	path := string(rest[:n])
	rest = rest[n+1:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	args := make([]string, 0, argc)
	for i := 0; i < argc && len(rest) > 0; i++ {
		n := bytes.IndexByte(rest, 0)
		if n < 0 {
			break
		}
		args = append(args, string(rest[:n]))
		rest = rest[n+1:]
	}
	return path, args
}
