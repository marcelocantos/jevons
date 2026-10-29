// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// 🎯T958 — one hung heavy gate must not idle the fleet.
//
// T603 serialises heavy runs so they do not collectively OOM the host. The
// 2026-09-30 incident showed the other side of that lease: a single full-suite
// holder (go test ./... -timeout 30m) held the lock while 15 narrow gates —
// single-package or -run subsets — waited out the full timeout. Nothing named
// the holder, workers relaunched the same gate, and a waiter was reaped as
// finished work. This file answers that shape without weakening T603's full-
// suite serialisation.

// Tunables. Tests shorten them; production values are the stated thresholds.
var (
	// heavyWaitReportEvery is how often a blocked waiter re-states the holder.
	heavyWaitReportEvery = 30 * time.Second
	// heavyNarrowBypassAge is how long a full-suite holder may sit with
	// waiters before a narrow run proceeds without the exclusive lease.
	heavyNarrowBypassAge = 5 * time.Minute
	// heavyStaleNoticeAge is how long a holder may sit with waiters before
	// one durable overseer notice is raised (not one per waiter).
	heavyStaleNoticeAge = 5 * time.Minute
	// heavyLeaseWaitOut receives wait/bypass/notice lines. Default stderr.
	heavyLeaseWaitOut io.Writer = os.Stderr
	// heavyWaitPoll is the flock retry interval while blocked.
	heavyWaitPoll = 100 * time.Millisecond
)

// HeavyLeaseWaiter is one process waiting on (or about to take) the heavy lease.
type HeavyLeaseWaiter struct {
	PID     int       `json:"pid"`
	Command []string  `json:"command"`
	CWD     string    `json:"cwd"`
	Started time.Time `json:"started"`
	Narrow  bool      `json:"narrow"`
}

func heavyWaitersDir(storeRoot string) string {
	return filepath.Join(storeRoot, "heavy.waiters")
}

func heavyWaiterPath(storeRoot string, pid int) string {
	return filepath.Join(heavyWaitersDir(storeRoot), fmt.Sprintf("%d.json", pid))
}

func heavyStaleNoticePath(storeRoot string) string {
	return filepath.Join(storeRoot, "heavy.stale-notice")
}

// NarrowCommand reports whether a heavy argv is a single-package or -run
// subset — the class that must not wait out an unrelated full-suite timeout
// (🎯T958). Non-heavy argv is never narrow. make/gmake test targets are full
// (not narrow): their package scope is not visible from argv alone.
func NarrowCommand(argv []string) bool {
	if !HeavyCommand(argv) {
		return false
	}
	if len(argv) == 0 {
		return false
	}
	switch filepath.Base(argv[0]) {
	case "make", "gmake":
		return false
	case "go":
		return narrowGoTest(argv[1:])
	default:
		return false
	}
}

func narrowGoTest(args []string) bool {
	i := 0
	for ; i < len(args); i++ {
		if args[i] == "test" {
			i++
			break
		}
	}
	if i == 0 {
		return false
	}
	hasRun := false
	packages := 0
	fullPackage := false
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			continue
		case a == "-run" || a == "-test.run":
			hasRun = true
			if i+1 < len(args) {
				i++
			}
		case strings.HasPrefix(a, "-run=") || strings.HasPrefix(a, "-test.run="):
			hasRun = true
		case strings.HasPrefix(a, "-"):
			if flagTakesValue(a) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		default:
			packages++
			if a == "./..." || a == "..." || a == "." {
				fullPackage = true
			}
		}
	}
	if hasRun {
		return true
	}
	// Zero packages = package under cwd → one package, narrow.
	// One concrete package (not ./...) → narrow.
	// Multiple packages or an explicit ./... → full suite.
	if packages == 0 {
		return true
	}
	if packages == 1 && !fullPackage {
		return true
	}
	return false
}

func flagTakesValue(a string) bool {
	if strings.Contains(a, "=") {
		return false
	}
	switch a {
	case "-count", "-timeout", "-parallel", "-bench", "-benchtime",
		"-blockprofile", "-coverprofile", "-cpu", "-cpuprofile",
		"-list", "-memprofile", "-mutexprofile", "-outputdir",
		"-tags", "-exec", "-vet", "-covermode", "-coverpkg":
		return true
	default:
		return false
	}
}

func registerHeavyWaiter(storeRoot string, w HeavyLeaseWaiter) {
	if storeRoot == "" || w.PID == 0 {
		return
	}
	_ = os.MkdirAll(heavyWaitersDir(storeRoot), 0o755)
	data, err := json.Marshal(w)
	if err != nil {
		return
	}
	_ = writeAtomic(heavyWaiterPath(storeRoot, w.PID), data, 0o600)
}

func unregisterHeavyWaiter(storeRoot string, pid int) {
	if storeRoot == "" || pid == 0 {
		return
	}
	_ = os.Remove(heavyWaiterPath(storeRoot, pid))
}

// listHeavyWaiters returns live waiter records. Dead PIDs are swept.
func listHeavyWaiters(storeRoot string) []HeavyLeaseWaiter {
	dir := heavyWaitersDir(storeRoot)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []HeavyLeaseWaiter
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var w HeavyLeaseWaiter
		if json.Unmarshal(data, &w) != nil || w.PID == 0 {
			continue
		}
		if !processAlive(w.PID) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		out = append(out, w)
	}
	return out
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// findDuplicateWaiter reports another live waiter (not self) with the same
// command and worktree. Identical re-queues from the same tree are refused
// rather than stacked (🎯T958).
func findDuplicateWaiter(storeRoot string, self HeavyLeaseWaiter) *HeavyLeaseWaiter {
	wantCmd := strings.Join(self.Command, "\x00")
	for _, w := range listHeavyWaiters(storeRoot) {
		if strings.Join(w.Command, "\x00") == wantCmd && w.CWD == self.CWD {
			cp := w
			return &cp
		}
	}
	return nil
}

func readHeavyLeaseOwner(storeRoot string) (*HeavyLeaseOwner, error) {
	data, err := os.ReadFile(heavyLockPath(storeRoot) + ".owner")
	if err != nil {
		return nil, err
	}
	var o HeavyLeaseOwner
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func ownerAge(o *HeavyLeaseOwner) time.Duration {
	if o == nil || o.Started.IsZero() {
		return 0
	}
	return time.Since(o.Started)
}

// ownerIsFullSuite is true when the recorded holder is not a narrow run.
// An owner with no command (AcquireHeavyLease with nil argv) counts as full:
// the conservative default keeps T603 serialisation.
func ownerIsFullSuite(o *HeavyLeaseOwner) bool {
	if o == nil {
		return true
	}
	if len(o.Command) == 0 {
		return true
	}
	return !NarrowCommand(o.Command)
}

func formatWaitMessage(owner *HeavyLeaseOwner, waiters int, waitingFor time.Duration) string {
	if owner == nil {
		return fmt.Sprintf("gate: waiting on heavy lease (holder unknown); waiters=%d waited=%s",
			waiters, waitingFor.Round(time.Second))
	}
	return fmt.Sprintf("gate: waiting on heavy lease: holder pid=%d command=%q cwd=%q held=%s waiters=%d waited=%s",
		owner.PID, owner.Command, owner.CWD, ownerAge(owner).Round(time.Second), waiters, waitingFor.Round(time.Second))
}

func reportWait(msg string) {
	if heavyLeaseWaitOut == nil {
		return
	}
	fmt.Fprintln(heavyLeaseWaitOut, msg)
}

// maybeStaleNotice writes at most one notice per holder token while waiters
// are present past the threshold. Durable under the store so a PO/overseer
// (or `gate queue`) can see it without lsof; blurter is best-effort.
func maybeStaleNotice(storeRoot string, owner *HeavyLeaseOwner, waiters int) {
	if owner == nil || waiters < 1 || ownerAge(owner) < heavyStaleNoticeAge {
		return
	}
	path := heavyStaleNoticePath(storeRoot)
	if prev, err := os.ReadFile(path); err == nil && strings.Contains(string(prev), "token="+owner.Token) {
		return
	}
	body := fmt.Sprintf("heavy lease stale: holder pid=%d command=%q cwd=%q held=%s waiters=%d token=%s\n",
		owner.PID, owner.Command, owner.CWD, ownerAge(owner).Round(time.Second), waiters, owner.Token)
	_ = writeAtomic(path, []byte(body), 0o600)
	reportWait("gate: " + strings.TrimSpace(body) + " (one notice; not per waiter)")
	if bin, err := exec.LookPath("blurter"); err == nil {
		cmd := exec.Command(bin, "send",
			"--app", "jevons-gate",
			"--severity", "problem",
			"--subject", "heavy lease held with waiters",
			"--body", strings.TrimSpace(body),
		)
		_ = cmd.Start()
	}
}

// resolveWaitCWD normalises the worktree path used for duplicate detection.
func resolveWaitCWD(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

func duplicateWaitError(w HeavyLeaseWaiter) error {
	return fmt.Errorf("gate: identical heavy gate already waiting (pid=%d cwd=%q); refusing duplicate queue entry", w.PID, w.CWD)
}

// heavyLeaseSelfWaiter builds the waiter record for this acquisition attempt.
func heavyLeaseSelfWaiter(command []string, cwd string) HeavyLeaseWaiter {
	return HeavyLeaseWaiter{
		PID:     os.Getpid(),
		Command: append([]string(nil), command...),
		CWD:     resolveWaitCWD(cwd),
		Started: time.Now(),
		Narrow:  NarrowCommand(command),
	}
}

// heavyLeaseNarrowBypass reports whether a blocked narrow run may proceed
// without the exclusive flock while a full-suite holder is stale.
func heavyLeaseNarrowBypass(command []string, owner *HeavyLeaseOwner) bool {
	if !NarrowCommand(command) {
		return false
	}
	if owner == nil || !ownerIsFullSuite(owner) {
		return false
	}
	return ownerAge(owner) >= heavyNarrowBypassAge
}

// heavyLeaseWaitTick emits wait/bypass/stale notices while blocked on flock.
// waited is how long this process has been waiting; lastReport is updated when
// a periodic line is emitted. Returns true when the caller should bypass.
func heavyLeaseWaitTick(storeRoot string, command []string, waited time.Duration, lastReport *time.Time) bool {
	owner, _ := readHeavyLeaseOwner(storeRoot)
	if heavyLeaseNarrowBypass(command, owner) {
		reportWait(fmt.Sprintf("gate: narrow heavy run bypassing full-suite holder pid=%d command=%q held=%s",
			owner.PID, owner.Command, ownerAge(owner).Round(time.Second)))
		return true
	}
	waiters := len(listHeavyWaiters(storeRoot))
	if lastReport == nil || lastReport.IsZero() || time.Since(*lastReport) >= heavyWaitReportEvery {
		reportWait(formatWaitMessage(owner, waiters, waited))
		maybeStaleNotice(storeRoot, owner, waiters)
		if lastReport != nil {
			*lastReport = time.Now()
		}
	}
	return false
}
