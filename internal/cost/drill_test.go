// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// burnerScript appends one big-usage assistant line per tick to its
// target JSONL, forever — a synthetic runaway worker. $1=file, $2=session.
const burnerScript = `f="$1"; sid="$2"; i=0
while true; do
  i=$((i+1))
  printf '{"type":"assistant","sessionId":"%s","requestId":"%s-%d","message":{"model":"claude-opus-4-8","usage":{"input_tokens":200000,"output_tokens":5000,"cache_creation_input_tokens":1000000,"cache_read_input_tokens":5000000}}}\n' "$sid" "$sid" "$i" >> "$f"
  sleep 0.2
done`

// TestDrillSyntheticRunawayKilled is 🎯T36 criterion 8: a synthetic
// runaway (N detached bypass-style burners in a launchd-style detached
// tmux server) is detected by the live collect→monitor→enforce loop and
// KILLED automatically within a bounded time, with the kill reaching the
// DETACHED processes. It runs against a PRIVATE tmux socket — never the
// real claudia fleet — so it is safe in CI and on a working machine.
func TestDrillSyntheticRunawayKilled(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed — drill requires it (CI installs tmux so criterion 8 runs)")
	}
	const nBurners = 5
	const budgetDeadline = 30 * time.Second

	dir := t.TempDir()
	// The socket lives in a SHORT temp dir of its own: macOS caps Unix
	// socket paths at ~104 chars, and t.TempDir() embeds the long test
	// name, which overflows it and makes tmux fail to bind.
	sockDir, err := os.MkdirTemp("", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "f.sock") // PRIVATE — not the real fleet
	scriptPath := filepath.Join(dir, "burner.sh")
	if err := os.WriteFile(scriptPath, []byte(burnerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(dir, "projects")

	// Detached tmux server hosting the runaway fleet — the anchor makes
	// the server outlive any one window, exactly like claudia-anchor.
	run := func(args ...string) error {
		return exec.Command("tmux", append([]string{"-S", sock}, args...)...).Run()
	}
	if err := run("new-session", "-d", "-s", "drill", "sh", "-c", "while true; do sleep 1; done"); err != nil {
		t.Fatalf("start drill tmux server: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", sock, "kill-server").Run() })

	for i := 0; i < nBurners; i++ {
		sid := "burner-" + strconv.Itoa(i) + "-0000-0000-000000000000"
		proj := filepath.Join(projects, "-work-burner"+strconv.Itoa(i))
		if err := os.MkdirAll(proj, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(proj, sid+".jsonl")
		if err := run("new-window", "-d", "-t", "drill", "sh", scriptPath, file, sid); err != nil {
			t.Fatalf("spawn burner %d: %v", i, err)
		}
	}

	// Snapshot the fleet's pane PIDs and prove they are DETACHED: their
	// parent is the tmux server, not this test process.
	panePIDs := drillPanePIDs(t, sock)
	if len(panePIDs) < nBurners {
		t.Fatalf("expected ≥%d fleet panes, got %d", nBurners, len(panePIDs))
	}
	for _, pid := range panePIDs {
		if ppid := parentPID(pid); ppid == os.Getpid() {
			t.Fatalf("burner %d is a child of the test process — not detached", pid)
		}
	}

	// Collector and monitor run on REAL time so the real burner events
	// land strictly inside the rolling window. Only the ENFORCER's
	// clock is controllable, and solely to drive it "unattended" (no
	// owner heartbeat) so the global-kill path fires the switch.
	store, err := OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	collector := NewCollector(&CollectorArgs{Store: store, ProjectsRoot: projects})
	cfg := DefaultBudgetConfig()
	monitor := NewMonitor(&MonitorArgs{
		Store: store, Config: func() *BudgetConfig { return cfg },
		CollectorLastPoll: collector.LastPoll,
	})

	var enfOffset time.Duration // 0 = attended; jumped later to go unattended
	ks := &TmuxKillSwitch{Socket: sock, Grace: 2 * time.Second}
	fired := 0
	enforcer := NewEnforcer(&EnforcerArgs{
		Snapshot: monitor.Snapshot,
		Config:   func() *BudgetConfig { return cfg },
		Actions: &drillActions{kill: func() error {
			fired++
			return ks.Kill()
		}},
		Now: func() time.Time { return time.Now().Add(enfOffset) },
	})

	// Let the runaway burn and the collector see it, until the store
	// holds a kill-level burn (bounded).
	deadline := time.Now().Add(budgetDeadline)
	burnUntil := time.Now().Add(6 * time.Second)
	for time.Now().Before(burnUntil) {
		if _, err := collector.ScanOnce(); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, err := collector.PollOnce(); err != nil {
			t.Fatalf("poll: %v", err)
		}
		if collectedEnough(t, monitor) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Confirm the monitor sees a runaway well past the kill threshold.
	snap, err := monitor.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.GlobalUSDPerHour < cfg.Global.KillUSDPerHour {
		t.Fatalf("drill did not produce a kill-level burn: %.2f USD/hr < %.2f",
			snap.GlobalUSDPerHour, cfg.Global.KillUSDPerHour)
	}

	// Go unattended: no owner contact for longer than the grace.
	enfOffset = attendedGrace + time.Minute

	// Enforce until the switch fires (confirmation needs ≥2 ticks), or
	// the bounded deadline passes.
	for fired == 0 && time.Now().Before(deadline) {
		if _, err := enforcer.Tick(); err != nil {
			t.Fatalf("enforce tick: %v", err)
		}
	}
	if fired == 0 {
		t.Fatal("clamp-down never fired the kill-switch within the deadline")
	}

	// The detached fleet must be dead within the bounded time.
	for time.Now().Before(deadline) {
		if len(survivors(panePIDs)) == 0 {
			return // success: detached runaway detected and killed automatically
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("kill-switch fired but %d detached burner(s) survived past the deadline: %v",
		len(survivors(panePIDs)), survivors(panePIDs))
}

// collectedEnough reports whether the store already holds a kill-level burn.
func collectedEnough(t *testing.T, m *Monitor) bool {
	t.Helper()
	snap, err := m.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snap.GlobalUSDPerHour >= DefaultBudgetConfig().Global.KillUSDPerHour
}

// drillActions routes every enforcement action to the same kill (the
// drill only needs the global kill-switch path exercised end to end).
type drillActions struct{ kill func() error }

func (d *drillActions) PauseWorker(string) error { return nil }
func (d *drillActions) KillWorker(string) error  { return d.kill() }
func (d *drillActions) StopFleet() error         { return nil }
func (d *drillActions) KillSwitch() error        { return d.kill() }

func drillPanePIDs(t *testing.T, sock string) []int {
	t.Helper()
	out, err := exec.Command("tmux", "-S", sock, "list-panes", "-a", "-F", "#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// parentPID returns a process's parent PID via ps, or -1.
func parentPID(pid int) int {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return -1
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return ppid
}
