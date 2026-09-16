// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T666 — a Grok billing 401 is answered by a one-shot Grok call.
//
// xAI issues a six-hour access token plus a refresh token; the CLI rotates
// the pair only when it starts, and claudia's billing fetch reads only the
// access token. So six hours after every `grok login` the ticker's Grok
// window died until the owner logged in again or a Grok seat happened to
// run. Verified 2026-09-16: with auth.json's expires_at in the past, one
// headless `grok -p` call rewrote the token and the next fetch answered.
// The reader now does exactly that on a 401, rate-limited, then re-polls.

// DefaultGrokRefreshWindow is the least time between two one-shots.
const DefaultGrokRefreshWindow = 10 * time.Minute

// grokOneShotTimeout bounds the headless call.
const grokOneShotTimeout = 90 * time.Second

// GrokBilling401 reports whether the readings carry a Grok billing 401 —
// the one unavailable reason a token rotation can fix.
func GrokBilling401(readings []claudia.PlanUsage) bool {
	for _, r := range readings {
		if r.Provider != claudia.ProviderGrok || r.Status != claudia.PlanUsageUnavailable {
			continue
		}
		if strings.Contains(r.Reason, "HTTP 401") {
			return true
		}
	}
	return false
}

// GrokTokenRefresher rotates the Grok login token. The product path runs the
// CLI; tests inject a fake.
type GrokTokenRefresher func(ctx context.Context) error

// DefaultGrokTokenRefresher runs one headless grok turn in a throwaway
// directory. The CLI checks auth.json on start and, with the token past
// expires_at, exchanges the refresh token and rewrites the file — the same
// path an interactive session takes. One tiny model call at most every
// DefaultGrokRefreshWindow.
func DefaultGrokTokenRefresher(ctx context.Context) error {
	bin, err := exec.LookPath("grok")
	if err != nil {
		return fmt.Errorf("grok CLI not on PATH: %w", err)
	}
	dir, err := os.MkdirTemp("", "jevons-grok-token-refresh-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, grokOneShotTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-p", "Reply with the single word OK.",
		"--max-turns", "1",
		"--permission-mode", "bypassPermissions",
	)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if len(tail) > 300 {
			tail = tail[len(tail)-300:]
		}
		return fmt.Errorf("grok one-shot: %w: %s", err, tail)
	}
	return nil
}

// grokRefreshDue reports whether a one-shot may run now, and records it.
func (r *Reader) grokRefreshDue(now time.Time) bool {
	window := r.args.GrokRefreshWindow
	if window <= 0 {
		window = DefaultGrokRefreshWindow
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastGrokRefresh.IsZero() && now.Sub(r.lastGrokRefresh) < window {
		return false
	}
	r.lastGrokRefresh = now
	return true
}

// refreshGrokTokenIfNeeded is the doRefresh tail: on a 401, run the one-shot
// and re-fetch once. Returns the readings to publish.
func (r *Reader) refreshGrokTokenIfNeeded(parent context.Context, readings []claudia.PlanUsage, refetch FetchFunc) []claudia.PlanUsage {
	if r == nil || !GrokBilling401(readings) || r.args.GrokRefreshDisabled {
		return readings
	}
	now := r.args.Now()
	if !r.grokRefreshDue(now) {
		return readings
	}
	refresher := r.args.GrokTokenRefresh
	if refresher == nil {
		refresher = DefaultGrokTokenRefresher
	}
	fields := map[string]any{"trigger": "grok billing HTTP 401"}
	if err := refresher(parent); err != nil {
		fields["outcome"] = "one_shot_failed"
		fields["err"] = err.Error()
		r.logEvent("grok_token_refresh", fields)
		slog.Warn("🎯T666 grok token refresh one-shot failed", "err", err)
		return readings
	}
	ctx, cancel := context.WithTimeout(parent, r.args.FetchTimeout)
	defer cancel()
	again, err := refetch(ctx)
	if err != nil {
		fields["outcome"] = "refetch_failed"
		fields["err"] = err.Error()
		r.logEvent("grok_token_refresh", fields)
		return readings
	}
	if GrokBilling401(again) {
		fields["outcome"] = "still_401"
	} else {
		fields["outcome"] = "recovered"
	}
	r.logEvent("grok_token_refresh", fields)
	slog.Info("🎯T666 grok token refreshed by one-shot", "outcome", fields["outcome"])
	return again
}

func (r *Reader) logEvent(decision string, fields map[string]any) {
	if r == nil || r.args.LogEvent == nil {
		return
	}
	r.args.LogEvent("plan_usage", decision, fields)
}
