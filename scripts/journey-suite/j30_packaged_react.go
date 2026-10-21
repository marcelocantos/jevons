// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// J30 exercises the actual packaged React composer and mux wire. No direct
// chat injection, Vite substitute, seeded reply, or API/transport interception.
func (s *suite) jPackagedReactOwnerTurn() error {
	return s.jPackagedReactScript("test.cjs", 4*time.Minute)
}

// J31 holds an actual provider tool while the owner submits a follow-up.
func (s *suite) jPackagedReactOwnerBoundary() error {
	return s.jPackagedReactScript("boundary.cjs", 8*time.Minute)
}

// J32 (🎯T789.1) sends and Cuts in on a busy seat over a transcript taller than 12,000 px.
func (s *suite) jPackagedReactSendCutIn() error {
	err := s.jPackagedReactScript("t789-live.cjs", 25*time.Minute)
	return errors.Join(err, s.cancelLeftoverOverseerWork("J32"))
}

// leftoverCancelWait bounds the cleanup of an overseer backlog a journey left.
const leftoverCancelWait = 3 * time.Minute

// cancelLeftoverOverseerWork cancels, as the owner would, the overseer work
// a journey left running, until the overseer is idle.
//
// J32 grows the transcript with up to twelve 150-line answers (moving on
// from any not answered within 120 s), then sends a 200-line request, two
// held messages and a Cut in. Its assertions end while that backlog still
// runs. In the 2026-10-20 green subset the overseer was still working it
// five minutes after J32 returned, so J33 could not start from an idle
// overseer. The backlog is J32's own, so J32 cancels it: an owner interrupt
// ends the running turn and 🎯T915 holds the queue for the owner's next send.
func (s *suite) cancelLeftoverOverseerWork(journey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), leftoverCancelWait+30*time.Second)
	defer cancel()
	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return fmt.Errorf("%s cleanup: %w", journey, err)
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return fmt.Errorf("%s cleanup: %w", journey, err)
	}
	go func() {
		for range frames {
		}
	}()
	deadline := time.Now().Add(leftoverCancelWait)
	for {
		if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "idle" }, 10*time.Second); err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return fmt.Errorf("%s cleanup: the overseer is still working its backlog after %s of owner interrupts: %w", journey, leftoverCancelWait, err)
		}
		if err := writeOwnerMux(ctx, conn, "interrupt", map[string]any{}); err != nil {
			return fmt.Errorf("%s cleanup: %w", journey, err)
		}
	}
}

// J33 (🎯T811): the daemon refuses the first owner delivery with the broker's
// real not_owner text (isolate-only fault seam); the packaged cockpit must show
// it undelivered with Resend, and Resend must deliver the same message once.
// Nothing reaches the development overseer or its grant.
func (s *suite) jUndeliveredResend() error {
	return s.jPackagedReactScript("t811-undelivered.cjs", 10*time.Minute)
}

// overseerIdleBefore bounds the wait for the shared overseer to finish an
// earlier journey's work before a journey that drives it starts.
const overseerIdleBefore = 5 * time.Minute

// drivesOverseer reports whether script sends owner turns to the shared
// overseer (boundary.cjs drives only its own aside).
func drivesOverseer(script string) bool { return script != "boundary.cjs" }

func (s *suite) jPackagedReactScript(script string, timeout time.Duration) error {
	provider := string(s.provider)
	var ready error
	// Brokered subscriptions already proved their sidecar during overseer
	// startup; a vendor CLI is not a prerequisite for that product path.
	if s.brokerSocket == "" {
		if provider == "cursor" {
			_, ready = exec.LookPath("cursor-agent")
		} else {
			ready = backendCLIReady(provider)
		}
	}
	if ready != nil {
		return &outageError{step: "J30 provider prerequisite", class: agenterr.ClassBackendUnavailable, msg: ready.Error()}
	}
	// The overseer is shared with every earlier journey. A journey that
	// drives it starts from an idle overseer, as J3 does (🎯T840): its owner
	// turn otherwise waits behind, or is steered into, work it did not send.
	// Gate 252cf6ac: J33's message, sent while J32's queued owner turn still
	// ran, took the 🎯T903 steer ladder ("agent_send name=jevons
	// status=steered … class=owner") and never reached the faulted
	// notify-queue handoff (seam fired 0x). In the 2026-10-20 J20→J30 green
	// run, the overseer J20's recovery re-minted was still working a
	// research brief when J30's owner turn arrived.
	if drivesOverseer(script) && !s.j45NoIdleWait {
		if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "idle" }, overseerIdleBefore); err != nil {
			return fmt.Errorf("overseer never settled before %s: %w", script, err)
		}
	}
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	root, err := j19RepoRoot()
	if err != nil {
		return err
	}
	// 🎯T832: the shared overseer is not a seat this journey minted. An
	// earlier journey may have moved it to another backend (J13 once
	// migrated it in place), so its expectation is its CURRENT provider,
	// and its latest launch must match that. The aside is minted here on the
	// requested provider and keeps the strict check.
	overseer, err := s.registryProvider(overseerName)
	if err != nil {
		return err
	}
	if seatprovider.Resolve(overseer).Plan != seatprovider.Resolve(s.provider).Plan {
		fmt.Printf("NOTE %s is on %s, not the suite's %s: an earlier journey moved it; its checks follow its current provider (T832)\n", overseerName, overseer, provider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	aside := "react-aside-" + uuid.NewString()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "react-ui-test", script), "--host", surface.host, "--provider", provider, "--overseer-provider", string(overseer), "--workdir", s.stateDir, "--aside", aside)
	asides := []string{aside}
	if script == "t789-live.cjs" {
		// Drives the overseer only; no aside is minted. The screenshot must
		// outlive the sandbox so the T493.1 visual verdict can be read.
		asides = nil
		cmd.Args = append(cmd.Args, "--screenshot", filepath.Join(os.TempDir(), "t789-live.png"))
	}
	if script == "t811-undelivered.cjs" {
		asides = nil
		// 🎯T920: the daemon log lets a failure say whether the fault fired.
		cmd.Args = append(cmd.Args, "--fault", filepath.Join(s.stateDir, "fault-owner-not-owner"),
			"--daemon-log", s.logPath,
			"--screenshot", filepath.Join(os.TempDir(), "t811-undelivered.png"))
	}
	if script == "boundary.cjs" {
		// The real sweep is every two minutes. Observe its completion, rather
		// than sleeping and assuming it happened; four minutes bounds outage.
		cmd.Args = append(cmd.Args, "--daemon-log", s.logPath, "--sweep-deadline-ms", fmt.Sprint((4 * time.Minute).Milliseconds()))
		// T627.4: the idle reaper's owner-visible case is the MCP-spawned aside.
		cmd.Args = append(cmd.Args, "--aside-only")
		// 🎯T540.7.1.1: keep the reload screenshots past the sandbox for the T493.1 verdict.
		cmd.Args = append(cmd.Args, "--screenshot", filepath.Join(os.TempDir(), "j31"))
	}
	// The browser needs no checkout as its working directory. Assets come
	// exclusively from the isolated daemon's embedded bundle.
	cmd.Dir = s.stateDir
	out, err := cmd.CombinedOutput()
	fmt.Fprint(os.Stdout, string(out))
	if err != nil {
		// A browser assertion timeout is a failed product observation, not
		// proof that the provider was unavailable. Prerequisites are above.
		return fmt.Errorf("packaged React conversation journey: %w\n%s", err, out)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Errorf("read runtime provider evidence: %w", err)
	}
	for _, name := range asides {
		if err := queueJourneyProvider(logs, name, provider); err != nil {
			return err
		}
	}
	return latestLaunchProvider(logs, overseerName, overseer)
}

// registryProvider is name's current provider as /api/agents reports it.
func (s *suite) registryProvider(name string) (claudia.Provider, error) {
	resp, err := http.Get("http://" + s.host + "/api/agents")
	if err != nil {
		return "", fmt.Errorf("read %s provider: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read %s provider: /api/agents HTTP %d", name, resp.StatusCode)
	}
	var agents []struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return "", fmt.Errorf("read %s provider: %w", name, err)
	}
	for _, a := range agents {
		if a.Name == name {
			if a.Provider == "" {
				return "", fmt.Errorf("%s has no provider in /api/agents", name)
			}
			return claudia.Provider(a.Provider), nil
		}
	}
	return "", fmt.Errorf("%s not in /api/agents (%d agents)", name, len(agents))
}

// latestLaunchProvider certifies a seat the journey did not mint (🎯T832):
// its most recent runtime launch must be on current, the provider the
// registry names now. Earlier launches on another backend are that seat's
// history, not this journey's evidence. /api/agents may name the sidecar
// runtime id (xai-oauth) where the launch log names the plan (grok), so
// both sides compare as plans (🎯T866.5).
func latestLaunchProvider(logs []byte, name string, current claudia.Provider) error {
	latest := ""
	for _, line := range strings.Split(string(logs), "\n") {
		event, err := queueJourneyLogFields(line)
		if err != nil || event["msg"] != "agent started" || event["name"] != name {
			continue
		}
		latest = event["provider"]
	}
	if latest == "" {
		return fmt.Errorf("no named runtime launch evidence for %s", name)
	}
	if seatprovider.Resolve(claudia.Provider(latest)).Plan != seatprovider.Resolve(current).Plan {
		return fmt.Errorf("agent %s last launched on %q, but the registry names %q", name, latest, current)
	}
	return nil
}
