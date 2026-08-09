// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// retroFirstDelay is how long after boot the first retrospective pass runs.
// Long enough that daemon start is not judged as friction; short enough that a
// restart does not postpone history analysis by a whole interval.
const retroFirstDelay = 5 * time.Minute

// RunRetroOnce executes one bounded retrospective history pass (🎯T353):
// mine git + eventlog tail + owner chat + session transcripts over the
// configured lookback, form coarse judgments, deliver sparsely to the overseer.
// Never advances the drip cursor; never writes bullseye.
func (c *Coach) RunRetroOnce(reason string) (CoachCycleResult, error) {
	if c == nil {
		return CoachCycleResult{}, fmt.Errorf("rsi coach: nil")
	}
	return c.retroCycle(reason)
}

// RetroState returns the durable record of the last retrospective pass.
func (c *Coach) RetroState() (RetroState, error) {
	if c == nil {
		return RetroState{}, fmt.Errorf("rsi coach: nil")
	}
	return LoadRetroState(c.args.StateDir)
}

func (c *Coach) retroCycle(reason string) (CoachCycleResult, error) {
	cfg, err := LoadCoachConfig(c.args.StateDir)
	if err != nil {
		return CoachCycleResult{}, err
	}
	manual := reason == "mcp" || reason == "test"
	if !manual && (!cfg.Enabled || !cfg.RetroEnabled) {
		return CoachCycleResult{}, nil
	}

	now := c.args.Now()
	workdir := c.args.RetroWorkdir
	if w := cfg.RetroWorkdir; w != "" {
		workdir = w
	}
	ev, stats, err := GatherRetroEvidence(RetroGatherArgs{
		Workdir:      workdir,
		EventLogPath: c.args.EventLogPath,
		ChatLogPath:  c.args.ChatLogPath,
		SessionsDir:  c.args.SessionsDir,
		Window:       cfg.RetroWindowAt(now),
	})
	if err != nil {
		return CoachCycleResult{}, err
	}

	prior, err := c.ledger.ActiveFingerprints(now)
	if err != nil {
		return CoachCycleResult{}, err
	}
	if _, err := c.dispositions.SyncOutcomes(ReadBullseyeTargetStatus, now); err != nil {
		slog.Debug("rsi retro outcome sync skipped", "err", err)
	}
	suppressions, err := c.dispositions.Suppressions(now)
	if err != nil {
		return CoachCycleResult{}, err
	}

	dry := c.args.DryRun || c.args.Deliverer == nil
	res, err := RunCoachCycle(CoachCycleArgs{
		Evidence:          ev,
		PriorFingerprints: prior,
		Deliverer:         c.args.Deliverer,
		// Fine sensors: extract clusters at the sensitive threshold …
		MinCount: retroExtractMinCount,
		// … coarse conclusions: the retro value bar and a tight rate cap decide
		// what is worth the overseer's attention.
		Retro:               true,
		RetroCoarseCount:    cfg.EffectiveRetroMinCount(),
		RateCap:             cfg.EffectiveRetroRateCap(),
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
			filed = append(filed, Filed{ID: "judgment", Name: j.Name, Fingerprint: j.Fingerprint})
		}
		if err := c.ledger.Record(filed, now); err != nil {
			slog.Warn("rsi retro judgment ledger failed", "err", err)
		}
		if err := c.dispositions.RecordDelivered(res.Delivered, now); err != nil {
			slog.Warn("rsi retro disposition record failed", "err", err)
		}
	}

	st, _ := LoadRetroState(c.args.StateDir)
	st.Runs++
	st.LastRunAt = now.Format(time.RFC3339)
	st.LastWindowStart = stats.WindowStart.Format(time.RFC3339)
	if stats.NewestCommit != "" {
		st.LastCommitSHA = stats.NewestCommit
		if !stats.NewestCommitAt.IsZero() {
			st.LastCommitAt = stats.NewestCommitAt.Format(time.RFC3339)
		}
	}
	st.LastCommits = stats.Commits
	st.LastEventRows = stats.EventRows
	st.LastChatTurns = stats.ChatTurns
	st.LastSessionTurns = stats.SessionTurns
	st.LastJudgments = len(res.Judgments)
	st.LastDelivered = len(res.Delivered)
	st.LastSuppressed = len(res.Skipped)
	if err := SaveRetroState(c.args.StateDir, st); err != nil {
		slog.Warn("rsi retro state save failed", "err", err)
	}

	if c.args.OnResult != nil {
		c.args.OnResult(res)
	}
	return res, nil
}

// runRetroSchedule is the batch-analysis cadence: rare, bounded passes over
// history. Separate from the drip ticker so observation stays fine-grained
// while conclusions stay coarse (🎯T353).
func (c *Coach) runRetroSchedule(ctx context.Context) {
	cfg, err := LoadCoachConfig(c.args.StateDir)
	if err != nil {
		<-ctx.Done()
		return
	}
	interval := cfg.RetroIntervalDuration()
	if interval <= 0 {
		<-ctx.Done()
		return
	}
	firstDelay := retroFirstDelay
	if interval < firstDelay {
		firstDelay = interval
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
			c.runRetroSafe("schedule_boot")
		case <-ticker.C:
			next, err := LoadCoachConfig(c.args.StateDir)
			if err != nil {
				continue
			}
			if d := next.RetroIntervalDuration(); d > 0 && d != interval {
				ticker.Stop()
				interval = d
				ticker = time.NewTicker(interval)
			}
			c.runRetroSafe("schedule")
		}
	}
}

// runRetroSafe runs one scheduled retrospective pass, subject to capacity
// admission (🎯T359). The backward pass is the coach's most expensive work and
// its cadence is measured in hours, so a deferred tick costs almost nothing.
func (c *Coach) runRetroSafe(reason string) {
	verdict, release := capacity.Ask(c.args.Capacity, capacity.ClassCoach, "retro_"+reason)
	defer release()
	if !verdict.Admitted() {
		slog.Info("rsi retro cycle deferred by capacity",
			"reason", reason, "verdict", verdict.Verdict, "cause", verdict.Reason,
			"pressure", verdict.Pressure.String(), "detail", verdict.Detail)
		return
	}
	res, err := c.retroCycle(reason)
	if err != nil {
		slog.Warn("rsi retro cycle failed", "reason", reason, "err", err)
		return
	}
	if len(res.Judgments) > 0 || len(res.Delivered) > 0 {
		slog.Info("rsi retro cycle",
			"reason", reason,
			"judgments", len(res.Judgments),
			"delivered", len(res.Delivered),
			"skipped", len(res.Skipped),
		)
	}
}
