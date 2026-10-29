// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// T1234's broker/replica slice has no seats: fixture mode deliberately does
// not load provider credentials or start the sidecar. J20's mint/park and J34's
// live-session assertions require a separate safe broker topology.
func TestT1234J20BrokerFixtureReplicatesBandsToAPI(t *testing.T) {
	base := "/tmp"
	if runtime.GOOS == "darwin" {
		base = "/private/tmp"
	}
	brokerDir, err := os.MkdirTemp(base, "jevons-t1234-broker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(brokerDir) })
	if err := os.Chmod(brokerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "claudia")
	build := exec.Command("go", "build", "-o", binary, "github.com/marcelocantos/claudia/cmd/claudia")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build private broker: %v: %s", err, out)
	}
	cmd := exec.Command(binary, "broker", "serve", "--state-dir", brokerDir, "--test-usage-fixture", "--no-resume")
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CLAUDIA_BROKER_SOCKET=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-finished
		}
	})
	socket := filepath.Join(brokerDir, "usage-fixture.sock")
	capPath := filepath.Join(brokerDir, "usage-fixture.cap")
	deadline := time.Now().Add(45 * time.Second)
	var secret []byte
	for time.Now().Before(deadline) {
		secret, err = os.ReadFile(capPath)
		if err == nil {
			if _, err = os.Stat(socket); err == nil {
				break
			}
		}
		select {
		case e := <-finished:
			t.Fatalf("fixture broker exited: %v: %s", e, logs.String())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("fixture broker not ready: %v: %s", err, logs.String())
	}
	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	replica, err := cost.OpenReplicaLinked(ctx, state, claudia.ControlSocketPath(filepath.Join(brokerDir, "broker.sock")))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer replica.Close()
	defer cancel()
	if _, err = planusage.OpenReplicaOverrides(replica, state); err != nil {
		t.Fatal(err)
	}
	projection, err := planusage.SubscribeProjection(ctx, replica, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", state)
	s.SetPlanUsageSource(func() any { return projection.JSON() })
	s.SetPlanSweep(func() any {
		// A synthetic reference exercises policy without minting any seat.
		return planusage.PlanActions(projection.Snapshot(), []planusage.AgentRef{{Name: "fixture-only", Provider: "grok", Purpose: "work"}}, time.Now(), planusage.DefaultThresholds())
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	api := httptest.NewServer(mux)
	defer api.Close()
	response, err := http.Get(api.URL + "/api/plan-usage/thresholds")
	if err != nil {
		t.Fatal(err)
	}
	var thresholds planusage.Thresholds
	if err := json.NewDecoder(response.Body).Decode(&thresholds); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || thresholds != planusage.DefaultThresholds() {
		t.Fatalf("thresholds HTTP %d: %+v", response.StatusCode, thresholds)
	}
	push := func(backends ...claudia.PlanUsage) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"backends": backends})
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		defer transport.CloseIdleConnections()
		req, _ := http.NewRequest(http.MethodPost, "http://fixture/usage", bytes.NewReader(raw))
		req.Header.Set("X-Fixture-Capability", string(secret))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("fixture HTTP %d: %s", resp.StatusCode, body)
		}
	}
	weekly := func(provider string, remaining, used float64, elapsed time.Duration) claudia.PlanUsage {
		reset := time.Now().Add(7*24*time.Hour - elapsed)
		return claudia.PlanUsage{Provider: claudia.Provider(provider), Status: claudia.PlanUsageAvailable, Windows: []claudia.PlanWindow{{Name: claudia.PlanWindowWeekly, RemainingPercent: &remaining, UsedPercent: &used, ResetsAt: &reset, LimitWindow: 7 * 24 * time.Hour}}}
	}
	await := func(provider string, band planusage.WeeklyBand, remaining float64) {
		t.Helper()
		end := time.Now().Add(10 * time.Second)
		for time.Now().Before(end) {
			resp, err := http.Get(api.URL + "/api/plan-usage")
			if err != nil {
				t.Fatal(err)
			}
			var snapshot planusage.Snapshot
			decodeErr := json.NewDecoder(resp.Body).Decode(&snapshot)
			resp.Body.Close()
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if be, ok := snapshot.Backend(provider); ok && len(be.Windows) > 0 && be.Windows[0].RemainingPercent != nil && *be.Windows[0].RemainingPercent == remaining && be.Windows[0].Band == string(band) && !be.Stale {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s did not replicate %s/%v through API: %s", provider, band, remaining, projection.JSON())
	}
	push(weekly("grok", 30, 70, 3*24*time.Hour+12*time.Hour))
	await("grok", planusage.BandAhead, 30)
	push(weekly("grok", 90, 10, 16*time.Hour))
	await("grok", planusage.BandOK, 90)
	names := []string{"grok", "claude", "codex", "cursor"}
	all := make([]claudia.PlanUsage, 0, len(names))
	for _, name := range names {
		all = append(all, weekly(name, 0, 100, 16*time.Hour))
	}
	push(all...)
	for _, name := range names {
		await(name, planusage.BandExhausted, 0)
	}
	sweep, err := http.Post(api.URL+"/api/plan-usage/sweep", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var actions []planusage.PlanAction
	if err := json.NewDecoder(sweep.Body).Decode(&actions); err != nil {
		t.Fatal(err)
	}
	sweep.Body.Close()
	if sweep.StatusCode != 200 || len(actions) != 1 || actions[0].Name != "fixture-only" || actions[0].To != "" || actions[0].Action != planusage.SeatPark {
		t.Fatalf("synthetic seat sweep HTTP %d actions=%+v", sweep.StatusCode, actions)
	}
	all = all[:0]
	for _, name := range names {
		all = append(all, weekly(name, 90, 10, 16*time.Hour))
	}
	push(all...)
	for _, name := range names {
		await(name, planusage.BandOK, 90)
	}
	t.Log(fmt.Sprintf("broker-owned J20 bands replicated to Jevons HTTP API; synthetic policy sweep returned %d park action", len(actions)))
}
