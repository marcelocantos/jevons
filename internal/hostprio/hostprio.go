// Package hostprio classifies process-priority samples for 🎯T779.
//
// Control-plane daemons (supervisord and its jevonsd / claudia / bullseye /
// mnemo children) must run at least at the scheduling priority of fleet
// agent shells. The live check is supervisor/check-control-plane-priority.sh;
// this package is the hermetic classifier over already-sampled rows so a
// regression does not need a running launchd job.
package hostprio

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// DefaultFleetBaseline is the measured Interactive-class floor for Aqua
// terminal / agent shells on macOS when no live fleet row is present.
const DefaultFleetBaseline = 31

// Kind is the coarse class of a sampled process.
type Kind string

const (
	KindControl Kind = "control"
	KindFleet   Kind = "fleet"
	KindOther   Kind = "other"
)

// Row is one ps-shaped sample: pid, ppid, pri, nice, command.
type Row struct {
	PID     int
	PPID    int
	PRI     int
	Nice    int
	Command string
}

// Report is the 🎯T779 comparison outcome.
type Report struct {
	OK          bool
	Reason      string // empty when OK; "starved" | "no_control_plane_rows"
	Baseline    int
	ControlMin  int
	ControlN    int
	FleetMax    int
	FleetN      int
	ControlRows []Row
	FleetRows   []Row
}

// ClassifyCommand returns the kind for a process command line.
func ClassifyCommand(cmd string) Kind {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return KindOther
	}
	if isControl(c) {
		return KindControl
	}
	if isFleet(c) {
		return KindFleet
	}
	return KindOther
}

func isControl(cmd string) bool {
	switch {
	case strings.Contains(cmd, "supervisord"):
		return true
	case strings.Contains(cmd, "/jevonsd") || strings.Contains(cmd, "bin/jevonsd"):
		return true
	case strings.Contains(cmd, "claudia broker"):
		return true
	case strings.Contains(cmd, "/bullseye serve") || strings.Contains(cmd, "bin/bullseye serve"):
		return true
	case strings.Contains(cmd, "/mnemo ") || strings.HasSuffix(cmd, "/mnemo") ||
		strings.Contains(cmd, "bin/mnemo"):
		return true
	case strings.Contains(cmd, "run-jevonsd.sh"),
		strings.Contains(cmd, "run-bullseye.sh"),
		strings.Contains(cmd, "run-mnemo.sh"),
		strings.Contains(cmd, "run-claudia.sh"):
		return true
	}
	return false
}

func isFleet(cmd string) bool {
	if (strings.Contains(cmd, "/zsh ") || strings.Contains(cmd, "/bash ") ||
		strings.HasPrefix(cmd, "zsh ") || strings.HasPrefix(cmd, "bash ")) &&
		(strings.Contains(cmd, "claude") || strings.Contains(cmd, "codex") ||
			strings.Contains(cmd, "cursor-agent") || strings.Contains(cmd, "agent_start") ||
			strings.Contains(cmd, "jevons-broker") || strings.Contains(cmd, "mcp-server")) {
		return true
	}
	if strings.Contains(cmd, "claude-code") || strings.Contains(cmd, "claude.app/Contents/MacOS/claude") {
		return true
	}
	if strings.Contains(cmd, "/bin/zsh -c ") || strings.Contains(cmd, "/bin/bash -lc ") {
		return true
	}
	return false
}

// ParsePSLines parses `ps -axo pid=,ppid=,pri=,ni=,command=` style lines.
func ParsePSLines(text string) ([]Row, error) {
	var rows []Row
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "PID") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pri, err3 := strconv.Atoi(fields[2])
		ni, err4 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		cmd := strings.Join(fields[4:], " ")
		rows = append(rows, Row{PID: pid, PPID: ppid, PRI: pri, Nice: ni, Command: cmd})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// Assess compares control-plane minimum PRI against a fleet baseline.
// baselineForce, when > 0, overrides the measured fleet max / default.
func Assess(rows []Row, baselineForce int) Report {
	var rep Report
	for _, r := range rows {
		switch ClassifyCommand(r.Command) {
		case KindControl:
			rep.ControlN++
			rep.ControlRows = append(rep.ControlRows, r)
			if rep.ControlN == 1 || r.PRI < rep.ControlMin {
				rep.ControlMin = r.PRI
			}
		case KindFleet:
			rep.FleetN++
			rep.FleetRows = append(rep.FleetRows, r)
			if rep.FleetN == 1 || r.PRI > rep.FleetMax {
				rep.FleetMax = r.PRI
			}
		}
	}
	switch {
	case baselineForce > 0:
		rep.Baseline = baselineForce
	case rep.FleetN > 0 && rep.FleetMax > 0:
		rep.Baseline = rep.FleetMax
	default:
		rep.Baseline = DefaultFleetBaseline
	}
	if rep.ControlN == 0 {
		rep.OK = false
		rep.Reason = "no_control_plane_rows"
		return rep
	}
	if rep.ControlMin < rep.Baseline {
		rep.OK = false
		rep.Reason = "starved"
		return rep
	}
	rep.OK = true
	return rep
}

// Summarize returns a one-line human verdict.
func Summarize(rep Report) string {
	if rep.OK {
		return fmt.Sprintf("PASS 🎯T779: control plane PRI %d >= baseline %d (n=%d)",
			rep.ControlMin, rep.Baseline, rep.ControlN)
	}
	if rep.Reason == "no_control_plane_rows" {
		return "FAIL 🎯T779: no control-plane process rows found"
	}
	return fmt.Sprintf("FAIL 🎯T779: control plane PRI %d < baseline %d (starved)",
		rep.ControlMin, rep.Baseline)
}
