// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// CoachArgs configures the ambient RSI coach harness (🎯T243).
// Product path: observe drip → judgments → overseer. Never bullseye file.
type CoachArgs struct {
	// StateDir holds coach config, cursor, and judgment ledger (required).
	StateDir string
	// EventLogPath defaults to eventlog.DefaultPath(StateDir).
	EventLogPath string
	// ChatLogPath is owner main chat (priority surface). Empty skips.
	ChatLogPath string
	// SessionsDir is provider session store. Empty skips.
	SessionsDir string
	// ChatLookback / SessionLookback / SessionTurnCap bound drip samples.
	ChatLookback    int
	SessionLookback int
	SessionTurnCap  int
	// Interval overrides config when non-zero on construct (daemon env).
	// Config file still wins after overseer retune if ReloadConfig each cycle.
	Interval time.Duration
	// Deliverer posts to overseer (required for non-dry delivery).
	Deliverer JudgmentDeliverer
	// DryRun never delivers.
	DryRun bool
	// SeedEOF when true seeds cursor to EOF on first load (avoid history flood).
	// The forward drip starts at EOF; history before it is reached by the
	// bounded retrospective pass (🎯T353), not by re-reading the journals.
	SeedEOF bool
	// RetroWorkdir is the git repository mined by the retrospective pass.
	// Empty (and no config override) = no git surface; other surfaces still run.
	RetroWorkdir string
	// Now optional clock.
	Now func() time.Time
	// OnResult optional hook after each cycle.
	OnResult func(CoachCycleResult)
}

// Coach is the ambient RSI coach schedule: drip-read surfaces, form judgments,
// deliver to overseer. Durable config is overseer-retunable.
type Coach struct {
	args         CoachArgs
	mu           sync.Mutex
	stream       []Evidence // optional stream buffer (reap, etc.)
	ledger       *Ledger    // judgment fingerprints (reuse mint ledger type)
	dispositions *DispositionStore
}

// NewCoach validates args and opens the judgment fingerprint ledger.
func NewCoach(args CoachArgs) (*Coach, error) {
	if strings.TrimSpace(args.StateDir) == "" {
		return nil, fmt.Errorf("rsi coach: StateDir required")
	}
	if args.EventLogPath == "" {
		args.EventLogPath = eventlog.DefaultPath(args.StateDir)
	}
	if args.Now == nil {
		args.Now = func() time.Time { return time.Now().UTC() }
	}
	if args.Interval == 0 {
		// Prefer durable config interval.
		if cfg, err := LoadCoachConfig(args.StateDir); err == nil {
			args.Interval = cfg.IntervalDuration()
		} else {
			args.Interval = DefaultCoachInterval
		}
	}
	// Judgment ledger at judged.json (not minted.json — mint residual separate).
	led, err := openLedgerAt(filepath.Join(args.StateDir, "rsi", "judged.json"), DefaultLedgerTTL)
	if err != nil {
		return nil, err
	}
	disp, err := OpenDispositionStore(args.StateDir)
	if err != nil {
		return nil, err
	}
	c := &Coach{args: args, ledger: led, dispositions: disp}
	if args.SeedEOF {
		_, _ = SeedCursorAtEOF(args.StateDir, args.ChatLogPath, args.EventLogPath, args.SessionsDir, DefaultSessionLookback)
	}
	return c, nil
}

// NoteReaped appends stream evidence (optional; coach may cluster with errors).
func (c *Coach) NoteReaped(threadIDs []string) {
	if c == nil || len(threadIDs) == 0 {
		return
	}
	ev := StreamReaped(threadIDs, c.args.Now())
	c.mu.Lock()
	c.stream = append(c.stream, ev...)
	const maxStream = 500
	if len(c.stream) > maxStream {
		c.stream = c.stream[len(c.stream)-maxStream:]
	}
	c.mu.Unlock()
}

// Run starts the coach schedule until ctx is done.
func (c *Coach) Run(ctx context.Context) {
	if c == nil {
		return
	}
	// Batch history analysis runs on its own (much slower) cadence.
	go c.runRetroSchedule(ctx)
	// Short first delay so boot is not blocked on a long interval.
	cfg, _ := LoadCoachConfig(c.args.StateDir)
	interval := c.args.Interval
	if cfg.IntervalSec != 0 || cfg.UpdatedAt != "" {
		interval = cfg.IntervalDuration()
	}
	if interval <= 0 {
		<-ctx.Done()
		return
	}
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
			if cfg, err := LoadCoachConfig(c.args.StateDir); err == nil && cfg.Enabled {
				c.runSafe("schedule_boot")
			}
		case <-ticker.C:
			// Bi-directional retune: reload interval from disk; restart ticker if changed.
			next, err := LoadCoachConfig(c.args.StateDir)
			if err != nil {
				continue
			}
			if !next.Enabled {
				continue
			}
			if d := next.IntervalDuration(); d > 0 && d != interval {
				ticker.Stop()
				interval = d
				ticker = time.NewTicker(interval)
			}
			c.runSafe("schedule")
		}
	}
}

// RunOnce executes one coach cycle immediately (MCP / tests).
func (c *Coach) RunOnce(reason string) (CoachCycleResult, error) {
	if c == nil {
		return CoachCycleResult{}, fmt.Errorf("rsi coach: nil")
	}
	return c.cycle(reason)
}

// Config returns the durable coach config (reload from disk).
func (c *Coach) Config() (CoachConfig, error) {
	if c == nil {
		return CoachConfig{}, fmt.Errorf("rsi coach: nil")
	}
	return LoadCoachConfig(c.args.StateDir)
}

// PatchConfig applies overseer retune and persists (bi-directional SI).
func (c *Coach) PatchConfig(patch CoachConfigPatch) (CoachConfig, error) {
	if c == nil {
		return CoachConfig{}, fmt.Errorf("rsi coach: nil")
	}
	if patch.Now.IsZero() {
		patch.Now = c.args.Now()
	}
	return PatchCoachConfig(c.args.StateDir, patch)
}

// StateDir returns the coach state directory.
func (c *Coach) StateDir() string {
	if c == nil {
		return ""
	}
	return c.args.StateDir
}

// Dispositions returns the durable judgment-disposition store (🎯T333).
func (c *Coach) Dispositions() *DispositionStore {
	if c == nil {
		return nil
	}
	return c.dispositions
}

func (c *Coach) runSafe(reason string) {
	res, err := c.cycle(reason)
	if err != nil {
		slog.Warn("rsi coach cycle failed", "reason", reason, "err", err)
		return
	}
	if len(res.Delivered) > 0 || len(res.Judgments) > 0 {
		slog.Info("rsi coach cycle",
			"reason", reason,
			"judgments", len(res.Judgments),
			"delivered", len(res.Delivered),
			"skipped", len(res.Skipped),
		)
	}
}

func (c *Coach) cycle(reason string) (CoachCycleResult, error) {
	cfg, err := LoadCoachConfig(c.args.StateDir)
	if err != nil {
		return CoachCycleResult{}, err
	}
	if !cfg.Enabled && reason != "mcp" && reason != "test" {
		return CoachCycleResult{}, nil
	}

	c.mu.Lock()
	stream := c.stream
	c.stream = nil
	c.mu.Unlock()

	cur, err := LoadCoachCursor(c.args.StateDir)
	if err != nil {
		return CoachCycleResult{}, err
	}

	var ev []Evidence
	// Priority: owner main chat drip.
	if chatTurns, next, err := DripChatLogTurns(c.args.ChatLogPath, cur, c.args.ChatLookback); err != nil {
		slog.Debug("rsi coach chatlog drip skipped", "err", err)
	} else {
		ev = append(ev, ChatTurnsToEvidence(chatTurns)...)
		cur = next
	}
	// Eventlog drip.
	if rows, next, err := DripEventLogRows(c.args.EventLogPath, cur, DefaultLookback); err != nil {
		slog.Debug("rsi coach eventlog drip skipped", "err", err)
	} else {
		ev = append(ev, EventsToEvidence(rows)...)
		cur = next
	}
	// Session transcript drip.
	if sessTurns, next, err := DripSessionTurns(c.args.SessionsDir, cur, c.args.SessionLookback, c.args.SessionTurnCap); err != nil {
		slog.Debug("rsi coach session drip skipped", "err", err)
	} else {
		ev = append(ev, ChatTurnsToEvidence(sessTurns)...)
		cur = next
	}
	ev = append(ev, stream...)

	cur.UpdatedAt = c.args.Now().Format(time.RFC3339)
	if err := SaveCoachCursor(c.args.StateDir, cur); err != nil {
		slog.Warn("rsi coach cursor save failed", "err", err)
	}

	prior, err := c.ledger.ActiveFingerprints(c.args.Now())
	if err != nil {
		return CoachCycleResult{}, err
	}

	// Outcome feed (🎯T333 #4): pull achieve/set_aside of filed targets into the
	// store, then suppress re-propose of concluded gaps without new evidence.
	if n, err := c.dispositions.SyncOutcomes(ReadBullseyeTargetStatus, c.args.Now()); err != nil {
		slog.Debug("rsi coach outcome sync skipped", "err", err)
	} else if n > 0 {
		slog.Info("rsi coach outcomes synced", "updated", n)
	}
	suppressions, err := c.dispositions.Suppressions(c.args.Now())
	if err != nil {
		return CoachCycleResult{}, err
	}

	dry := c.args.DryRun || c.args.Deliverer == nil
	res, err := RunCoachCycle(CoachCycleArgs{
		Evidence:            ev,
		PriorFingerprints:   prior,
		Deliverer:           c.args.Deliverer,
		MinCount:            cfg.EffectiveMinCount(),
		RateCap:             cfg.EffectiveRateCap(),
		FocusFilters:        cfg.FocusFilters,
		OutcomeSuppressions: suppressions,
		DryRun:              dry,
	})
	if err != nil {
		return res, err
	}
	if len(res.Delivered) > 0 {
		filed := make([]Filed, 0, len(res.Delivered))
		for _, j := range res.Delivered {
			filed = append(filed, Filed{
				ID:          "judgment",
				Name:        j.Name,
				Fingerprint: j.Fingerprint,
			})
		}
		if err := c.ledger.Record(filed, c.args.Now()); err != nil {
			slog.Warn("rsi coach judgment ledger failed", "err", err)
		}
		// Disposition loop (🎯T333 #1): every delivered judgment starts pending.
		if err := c.dispositions.RecordDelivered(res.Delivered, c.args.Now()); err != nil {
			slog.Warn("rsi coach disposition record failed", "err", err)
		}
	}
	if c.args.OnResult != nil {
		c.args.OnResult(res)
	}
	_ = reason
	return res, nil
}
