// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// Topology B (claudia 🎯T234): an ordinary isolated credential-capable
// broker accepts a test-only per-provider usage override. The socket and
// capability live under the broker's private 0700 state directory — never
// the retired JEVONS_PLAN_USAGE_FIXTURE env, and never the shared feed.
const (
	usageOverrideFlag       = "--usage-fixture-override=claude,codex,grok,cursor"
	usageOverrideSubdir     = "usage-fixture-override"
	usageOverrideSocketName = "control.sock"
	usageOverrideCapability = "capability"
	usageOverrideWait       = 10 * time.Second
)

func isolatedBrokerStateDir(root string) string {
	return filepath.Join(root, "state")
}

func isolatedBrokerSocketPath(root string) string {
	return filepath.Join(isolatedBrokerStateDir(root), "broker.sock")
}

func isolatedBrokerMCPSeed(root string) string {
	return filepath.Join(isolatedBrokerStateDir(root), "mcp-seed")
}

func (b *isolatedBroker) overrideDir() string {
	return filepath.Join(isolatedBrokerStateDir(b.root), usageOverrideSubdir)
}

func (b *isolatedBroker) overrideSocket() string {
	return filepath.Join(b.overrideDir(), usageOverrideSocketName)
}

func (b *isolatedBroker) overrideCapabilityPath() string {
	return filepath.Join(b.overrideDir(), usageOverrideCapability)
}

// overrideWeekly is one published weekly window. elapsed is how far into
// the week the reading sits (same shape as claudia's T234 fixtureWeekly).
func overrideWeekly(provider string, remaining, used float64, elapsed time.Duration) claudia.PlanUsage {
	reset := time.Now().UTC().Add(7*24*time.Hour - elapsed)
	return claudia.PlanUsage{
		Provider: claudia.Provider(provider),
		Status:   claudia.PlanUsageAvailable,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			RemainingPercent: &remaining,
			UsedPercent:      &used,
			ResetsAt:         &reset,
			LimitWindow:      time.Duration(planusage.DefaultWeeklyWindowSeconds) * time.Second,
		}},
	}
}

func healthyOverrideReadings() []claudia.PlanUsage {
	return overrideReadings(map[string][2]float64{
		"claude": {defaultPlanRemaining, defaultPlanUsed},
		"codex":  {defaultPlanRemaining, defaultPlanUsed},
		"grok":   {defaultPlanRemaining, defaultPlanUsed},
		"cursor": {defaultPlanRemaining, defaultPlanUsed},
	}, 16*time.Hour)
}

// destEmptyOverrideReadings makes grok ahead and the other three exhausted
// so Claudia has no destination plan.
func destEmptyOverrideReadings() []claudia.PlanUsage {
	return []claudia.PlanUsage{
		overrideWeekly("grok", 30, 70, 3*24*time.Hour+12*time.Hour),
		overrideWeekly("claude", 0, 100, 16*time.Hour),
		overrideWeekly("codex", 0, 100, 16*time.Hour),
		overrideWeekly("cursor", 0, 100, 16*time.Hour),
	}
}

func grokDestOverrideReadings() []claudia.PlanUsage {
	return overrideReadings(map[string][2]float64{
		"grok":   {defaultPlanRemaining, defaultPlanUsed},
		"claude": {0, 100},
		"codex":  {0, 100},
		"cursor": {0, 100},
	}, 16*time.Hour)
}

func exhaustedOverrideReadings() []claudia.PlanUsage {
	return overrideReadings(map[string][2]float64{
		"claude": {0, 100},
		"codex":  {0, 100},
		"grok":   {0, 100},
		"cursor": {0, 100},
	}, 16*time.Hour)
}

func hotSwitchOverrideReadings(dest string) []claudia.PlanUsage {
	elapsed := 16 * time.Hour
	out := make([]claudia.PlanUsage, 0, 4)
	for _, name := range []string{"claude", "codex", "grok", "cursor"} {
		if name == "claude" {
			out = append(out, overrideWeekly(name, 0, 100, elapsed))
			continue
		}
		if name == dest {
			out = append(out, overrideWeekly(name, defaultPlanRemaining, defaultPlanUsed, elapsed))
			continue
		}
		out = append(out, overrideWeekly(name, 0, 100, elapsed))
	}
	return out
}

func overrideReadings(remainingUsed map[string][2]float64, elapsed time.Duration) []claudia.PlanUsage {
	names := []string{"claude", "codex", "grok", "cursor"}
	out := make([]claudia.PlanUsage, 0, len(names))
	for _, name := range names {
		pair := remainingUsed[name]
		out = append(out, overrideWeekly(name, pair[0], pair[1], elapsed))
	}
	return out
}

func prepareIsolatedBrokerState(root string) error {
	for _, dir := range []string{isolatedBrokerStateDir(root), isolatedBrokerMCPSeed(root)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (b *isolatedBroker) waitOverrideReady(deadline time.Time) error {
	if b == nil {
		return fmt.Errorf("usage override: no isolated broker")
	}
	var last error
	for time.Now().Before(deadline) {
		select {
		case err := <-b.done:
			b.cmd = nil
			return fmt.Errorf("isolated Claudia broker exited before usage override ready: %w", err)
		default:
		}
		secret, err := os.ReadFile(b.overrideCapabilityPath())
		if err == nil && len(secret) > 0 {
			if _, err = os.Stat(b.overrideSocket()); err == nil {
				return nil
			}
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return fmt.Errorf("usage override not ready under %s: %w", b.overrideDir(), last)
}

func (b *isolatedBroker) pushUsage(readings []claudia.PlanUsage) error {
	if b == nil {
		return fmt.Errorf("usage override: no isolated broker")
	}
	secret, err := os.ReadFile(b.overrideCapabilityPath())
	if err != nil {
		return fmt.Errorf("usage override capability: %w", err)
	}
	raw, err := json.Marshal(map[string]any{"backends": readings})
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", b.overrideSocket())
	}}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodPost, "http://override/usage", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("X-Fixture-Capability", string(secret))
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("usage override POST: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("usage override HTTP %d: %s", resp.StatusCode, trim(string(body), 300))
	}
	return nil
}

func (s *suite) waitPlanRemaining(provider string, remaining float64) error {
	if s.host == "" {
		return fmt.Errorf("wait plan usage: no isolate host")
	}
	deadline := time.Now().Add(usageOverrideWait)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + s.host + "/api/plan-usage")
		if err != nil {
			last = err.Error()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			last = fmt.Sprintf("HTTP %d", resp.StatusCode)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var snap planusage.Snapshot
		decodeErr := json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		if decodeErr != nil {
			last = decodeErr.Error()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if be, ok := snap.Backend(provider); ok && len(be.Windows) > 0 && be.Windows[0].RemainingPercent != nil &&
			*be.Windows[0].RemainingPercent == remaining && !be.Stale {
			return nil
		}
		last = fmt.Sprintf("%+v", snap)
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s remaining %v did not replicate through isolate /api/plan-usage: %s", provider, remaining, last)
}

func (s *suite) pushAndAwait(readings []claudia.PlanUsage) error {
	if s.broker == nil {
		return fmt.Errorf("usage override: isolate has no broker")
	}
	if err := s.broker.pushUsage(readings); err != nil {
		return err
	}
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	for _, u := range readings {
		if u.Status != claudia.PlanUsageAvailable || len(u.Windows) == 0 || u.Windows[0].RemainingPercent == nil {
			continue
		}
		if err := s.waitPlanRemaining(string(u.Provider), *u.Windows[0].RemainingPercent); err != nil {
			return err
		}
	}
	return nil
}

// ensureIsolatedBroker starts a T234 override broker on this isolate when
// the suite is in direct mode (no primary broker). J20 mints live seats, so
// the broker must be credential-capable — not --test-usage-fixture.
func (s *suite) ensureIsolatedBroker() error {
	if s.broker != nil {
		return nil
	}
	b, err := s.startIsolatedBroker()
	if err != nil {
		return err
	}
	s.broker = b
	s.brokerSocket = b.socket
	return s.bounceDrain()
}
