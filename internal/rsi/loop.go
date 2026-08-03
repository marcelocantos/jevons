// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// DefaultInterval is how often the ambient schedule runs when enabled.
const DefaultInterval = 30 * time.Minute

// DefaultLookback is the evidence window tailed from the event journal.
const DefaultLookback = 200

// LoopArgs configures the ambient RSI schedule/stream loop (daemon).
type LoopArgs struct {
	// StateDir holds the mint ledger (required).
	StateDir string
	// MintCwd is the bullseye repo to file into. Empty disables filing (extract+log only).
	MintCwd string
	// EventLogPath defaults to eventlog.DefaultPath(StateDir).
	EventLogPath string
	// Interval defaults to DefaultInterval. ≤0 disables the schedule ticker
	// (stream-only / manual RunOnce still work).
	Interval time.Duration
	// MinCount / MaxMint forwarded to RunCycle (0 = package defaults).
	MinCount int
	MaxMint  int
	// DryRun never files; still logs proposed candidates.
	DryRun bool
	// Filer overrides BullseyeFiler when non-nil.
	Filer Filer
	// Now is optional clock for tests.
	Now func() time.Time
	// OnResult is optional hook after each cycle (tests / metrics).
	OnResult func(CycleResult)
}

// Loop is the ambient RSI harness: periodic schedule + stream nudge.
type Loop struct {
	args   LoopArgs
	mu     sync.Mutex
	stream []Evidence
	ledger *Ledger
}

// NewLoop validates args and opens the mint ledger.
func NewLoop(args LoopArgs) (*Loop, error) {
	if strings.TrimSpace(args.StateDir) == "" {
		return nil, fmt.Errorf("rsi: StateDir required")
	}
	if args.Interval == 0 {
		args.Interval = DefaultInterval
	}
	if args.EventLogPath == "" {
		args.EventLogPath = eventlog.DefaultPath(args.StateDir)
	}
	if args.Now == nil {
		args.Now = func() time.Time { return time.Now().UTC() }
	}
	if args.Filer == nil {
		args.Filer = BullseyeFiler{}
	}
	led, err := OpenLedger(args.StateDir, DefaultLedgerTTL)
	if err != nil {
		return nil, err
	}
	return &Loop{args: args, ledger: led}, nil
}

// NoteReaped appends stream evidence from idle GC (non-blocking, best-effort).
func (l *Loop) NoteReaped(threadIDs []string) {
	if l == nil || len(threadIDs) == 0 {
		return
	}
	ev := StreamReaped(threadIDs, l.args.Now())
	l.mu.Lock()
	l.stream = append(l.stream, ev...)
	const maxStream = 500
	if len(l.stream) > maxStream {
		l.stream = l.stream[len(l.stream)-maxStream:]
	}
	l.mu.Unlock()
}

// Run starts the schedule until ctx is done. Interval ≤0 means no ticker
// (caller may still invoke RunOnce). Safe to call from a goroutine.
func (l *Loop) Run(ctx context.Context) {
	if l == nil {
		return
	}
	interval := l.args.Interval
	if interval <= 0 {
		<-ctx.Done()
		return
	}
	// Short first delay so boot is not blocked on mint.
	firstDelay := interval
	if firstDelay > 2*time.Minute {
		firstDelay = 2 * time.Minute
	}
	first := time.NewTimer(firstDelay)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			l.runSafe("schedule_boot")
		case <-ticker.C:
			l.runSafe("schedule")
		}
	}
}

// RunOnce executes one retrospective cycle immediately.
func (l *Loop) RunOnce(reason string) (CycleResult, error) {
	if l == nil {
		return CycleResult{}, fmt.Errorf("rsi: nil loop")
	}
	return l.cycle(reason)
}

func (l *Loop) runSafe(reason string) {
	res, err := l.cycle(reason)
	if err != nil {
		slog.Warn("ambient RSI cycle failed", "reason", reason, "err", err)
		return
	}
	if len(res.Filed) > 0 || len(res.Proposed) > 0 {
		slog.Info("ambient RSI cycle",
			"reason", reason,
			"proposed", len(res.Proposed),
			"filed", len(res.Filed),
			"skipped", len(res.Skipped),
		)
	}
}

func (l *Loop) cycle(reason string) (CycleResult, error) {
	l.mu.Lock()
	stream := l.stream
	l.stream = nil
	l.mu.Unlock()

	rows, err := loadErrorishEvents(l.args.EventLogPath, DefaultLookback)
	if err != nil {
		return CycleResult{}, err
	}
	ev := EventsToEvidence(rows)
	ev = append(ev, stream...)

	prior, err := l.ledger.ActiveFingerprints(l.args.Now())
	if err != nil {
		return CycleResult{}, err
	}

	dry := l.args.DryRun || strings.TrimSpace(l.args.MintCwd) == ""
	if !dry && !hasBullseye(l.args.MintCwd) {
		slog.Debug("ambient RSI: no bullseye.yaml; dry-run only", "cwd", l.args.MintCwd)
		dry = true
	}

	res, err := RunCycle(CycleArgs{
		Cwd:               l.args.MintCwd,
		Evidence:          ev,
		PriorFingerprints: prior,
		Filer:             l.args.Filer,
		MinCount:          l.args.MinCount,
		MaxMint:           l.args.MaxMint,
		DryRun:            dry,
	})
	if err != nil {
		return res, err
	}
	if len(res.Filed) > 0 {
		if err := l.ledger.Record(res.Filed, l.args.Now()); err != nil {
			slog.Warn("ambient RSI ledger record failed", "err", err)
		}
	}
	if l.args.OnResult != nil {
		l.args.OnResult(res)
	}
	_ = reason
	return res, nil
}

func loadErrorishEvents(path string, limit int) ([]EventRow, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	events, err := eventlog.Tail(path, eventlog.TailOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]EventRow, 0, len(events))
	for _, e := range events {
		outcome, _ := e.Fields["outcome"].(string)
		row := EventRow{
			TS:        e.TS,
			Level:     e.Level,
			Msg:       e.Msg,
			Component: e.Component,
			Decision:  e.Decision,
			Outcome:   outcome,
			Corr:      e.Corr,
		}
		if len(e.Fields) > 0 {
			row.Fields = map[string]string{}
			for k, v := range e.Fields {
				row.Fields[k] = fmt.Sprint(v)
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func hasBullseye(cwd string) bool {
	if strings.TrimSpace(cwd) == "" {
		return false
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	for i := 0; i < 8; i++ {
		if st, err := os.Stat(filepath.Join(dir, "bullseye.yaml")); err == nil && !st.IsDir() {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}
