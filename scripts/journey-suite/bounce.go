// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// isolateDaemonEnv keeps the throwaway daemon off the host claudia
// broker and out of the owner's grok-homes. Most journeys use direct mode
// so J14 can observe a SIGINT drain (🎯T63); migration journeys use their
// own disposable broker. Sharing XDG_STATE_HOME would resume the owner's
// conversations (🎯T627.1).
func (s *suite) isolateDaemonEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "CLAUDIA_NO_BROKER=") ||
			strings.HasPrefix(entry, "CLAUDIA_BROKER_SOCKET=") ||
			strings.HasPrefix(entry, "CLAUDIA_OMP_SOCKET=") ||
			strings.HasPrefix(entry, "JEVONS_SPOOL_DIR=") ||
			strings.HasPrefix(entry, "XDG_STATE_HOME=") {
			continue
		}
		env = append(env, entry)
	}
	if s.brokerSocket == "" {
		env = append(env, "CLAUDIA_NO_BROKER=1")
	} else {
		env = append(env, "CLAUDIA_NO_BROKER=0", "CLAUDIA_BROKER_SOCKET="+s.brokerSocket,
			"CLAUDIA_OMP_SOCKET="+filepath.Join(filepath.Dir(s.brokerSocket), "omp.sock"))
	}
	env = append(env,
		"XDG_STATE_HOME="+s.stateDir,
		"JEVONS_SPOOL_DIR="+filepath.Join(s.stateDir, "spool"),
		// 🎯T811: arms the daemon's isolate-only broker fault seam; inert
		// until a journey writes the fault file.
		"JEVONS_TEST_FAULTS=1",
	)
	// 🎯T625.6: hermetic plan feed; the journey's own daemonEnv follows
	// and, being later, overrides it.
	planEnv, err := defaultPlanFixtureEnv(s.stateDir, string(s.provider))
	if err != nil {
		panic(fmt.Sprintf("isolate plan fixture: %v", err))
	}
	env = append(env, planEnv)
	return append(env, s.daemonEnv...)
}

func (s *suite) startDaemon() error {
	if s == nil {
		return fmt.Errorf("start daemon: no suite")
	}
	// 🎯T526: refuse to start when the isolate port is already held.
	// Otherwise a bind failure exits the child while waitReady adopts the
	// foreign daemon and journey MCP mints into daily ~/.jevons.
	if err := portguard.ErrIfPortHeld(s.port); err != nil {
		return err
	}
	cmd := exec.Command(s.daemonBin,
		"-config", s.cfgPath,
		"-port", fmt.Sprint(s.port),
		"-bind", "127.0.0.1",
		"-workdir", s.workdir,
	)
	cmd.Stdout = s.logFile
	cmd.Stderr = s.logFile
	cmd.Dir = s.workdir
	cmd.Env = s.isolateDaemonEnv()
	// 🎯T837: the log is shared by every start in the run; the stop reason
	// is read from this start's bytes only.
	var logStart int64
	if fi, err := os.Stat(s.logPath); err == nil {
		logStart = fi.Size()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	s.cmd = cmd
	s.cmdWait = waitCh
	wait := readyTimeout
	if s.readyWait > 0 {
		wait = s.readyWait
	}
	if err := s.waitReadyOwned(wait); err != nil {
		// Read the cause BEFORE signalling: an interrupted launch logs
		// "auto-start failed ... context canceled", which is the teardown
		// talking, not the reason the overseer was stopped.
		var notRunning *overseerNotRunningError
		if errors.As(err, &notRunning) {
			err = &isolateStartError{err: err, cause: s.overseerStopCause(logStart, time.Now(), wait)}
		}
		_ = s.signalStop(2 * time.Second)
		return err
	}
	return nil
}

// overseerStopCause names why the overseer is stopped from the isolate
// daemon's log written since offset (🎯T837).
func (s *suite) overseerStopCause(offset int64, gaveUp time.Time, budget time.Duration) string {
	body, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Sprintf("overseer %s stopped: cause unknown — isolate log unreadable: %v", overseerName, err)
	}
	if offset < 0 || offset > int64(len(body)) {
		offset = 0
	}
	return overseerStopReason(body[offset:], overseerName, gaveUp, budget)
}

func (s *suite) signalStop(timeout time.Duration) error {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	signalErr := s.cmd.Process.Signal(os.Interrupt)
	waitCh := s.cmdWait
	if waitCh == nil {
		waitCh = make(chan error, 1)
		cmd := s.cmd
		go func() { waitCh <- cmd.Wait() }()
	}
	select {
	case err := <-waitCh:
		s.cmd = nil
		s.cmdWait = nil
		if signalErr != nil {
			return fmt.Errorf("signal daemon: %w", signalErr)
		}
		if err != nil {
			return fmt.Errorf("daemon exited while draining: %w", err)
		}
		return nil
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-waitCh
		s.cmd = nil
		s.cmdWait = nil
		return fmt.Errorf("daemon did not exit after interrupt")
	}
}

// waitReadyOwned is waitReady plus 🎯T526: ready only when OUR child is the
// TCP listener. Foreign /health must not count — that is how J20 minted into
// a leftover :13715 on daily ~/.jevons after "address already in use".
func (s *suite) waitReadyOwned(d time.Duration) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return fmt.Errorf("wait ready: no daemon process")
	}
	ourPID := s.cmd.Process.Pid
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		if s.cmdWait != nil {
			select {
			case err := <-s.cmdWait:
				s.cmd = nil
				s.cmdWait = nil
				if err == nil {
					err = fmt.Errorf("exit 0")
				}
				return fmt.Errorf("daemon exited before ready (likely bind failure); refusing foreign listener on %s: %w", s.host, err)
			default:
			}
		}
		if err := portguard.ErrIfForeignListener(s.port, ourPID); err != nil {
			return err
		}
		if pid, err := portguard.ListenPID(s.port); err == nil && pid == ourPID {
			if err := probeReady(s.host); err == nil {
				return nil
			} else {
				last = err
			}
		} else if err != nil {
			last = err
		}
		time.Sleep(400 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return last
}

func (s *suite) bounceDrain() error {
	if err := s.signalStop(8 * time.Second); err != nil {
		// Restart anyway — a stuck child still occupies the port.
		_ = err
	}
	return s.startDaemon()
}

func (s *suite) agentsPath() string {
	return filepath.Join(s.stateDir, "agents.json")
}

func (s *suite) handoverPath(name string) string {
	return filepath.Join(s.stateDir, "handover", name+".json")
}

// The normal registry loader treats read failures as an empty registry. An
// oracle must distinguish failed observation from an actual missing seat.
func bounceRegistrySnapshot(path string) (map[string]claudia.AgentDef, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var defs []claudia.AgentDef
	if err := json.Unmarshal(body, &defs); err != nil {
		return nil, err
	}
	if defs == nil {
		return nil, fmt.Errorf("registry is null, not an agent list")
	}
	out := make(map[string]claudia.AgentDef, len(defs))
	for _, def := range defs {
		if _, duplicate := out[def.Name]; duplicate || def.Name == "" {
			return nil, fmt.Errorf("duplicate or empty registry name %q", def.Name)
		}
		out[def.Name] = def
	}
	return out, nil
}

// J14 exercises a normal SIGINT drain and restart, not crash or SIGHUP adoption.
// Stable registry IDs alone cannot prove an agent resumed: require a fresh
// post-restart answer that uses a fact supplied only before the restart.
func (s *suite) jBounceResume() error {
	// J13 intentionally migrates the overseer. Use a fresh explicitly selected
	// aside so full-suite ordering cannot certify that other backend as ours.
	id := "bounce-aside-" + uuid.NewString()
	work, err := os.MkdirTemp(s.stateDir, "bounce-work-")
	if err != nil {
		return err
	}
	defer func() { _, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id}) }()
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "provider": string(s.provider),
		"description": "disposable normal-drain continuity probe",
	}); err != nil {
		if outage := asOutage("bounce fixture spawn", err); outage != nil {
			return outage
		}
		return fmt.Errorf("bounce fixture spawn: %w", err)
	}
	direct := func(prompt, expected string) error {
		out, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": prompt})
		if outage := asOutage("bounce direct", err); outage != nil {
			return outage
		}
		if err != nil {
			return err
		}
		if outage := replyOutage("bounce direct reply", out); outage != nil {
			return outage
		}
		if !bounceDirectMatches(out, expected) {
			return fmt.Errorf("direct reply %q differs from requested %q", trim(out, 200), expected)
		}
		return nil
	}
	// 🎯T834: the aside's reply notice below races the owner turn for the
	// overseer, so a stall diagnosis reads the daemon log from here.
	logStart := s.logSize()
	secret := "bounce-memory-" + uuid.NewString()
	ack := "bounce-stored-" + uuid.NewString()
	seed := "Remember this journey continuity secret for my next question: " + secret + ". Reply with exactly: " + ack
	if err := direct(seed, ack); err != nil {
		return fmt.Errorf("pre-bounce seed turn: %w", err)
	}
	if err := s.completeBounceOwnerTurn(logStart); err != nil {
		return err
	}

	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("snapshot before bounce: %w", err)
	}
	if before[id].SessionID == "" || before[id].Provider != s.provider {
		return fmt.Errorf("bounce fixture lacks its selected provider/session identity")
	}
	if before[overseerName].SessionID == "" {
		return fmt.Errorf("bounce owner conversation lacks a provider session identity")
	}
	preLogs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if err := queueJourneyProvider(preLogs, id, string(s.provider)); err != nil {
		return fmt.Errorf("original aside: %w", err)
	}
	handovers := map[string]string{}
	for name := range before {
		body, err := os.ReadFile(s.handoverPath(name))
		if err == nil {
			if name == id {
				return fmt.Errorf("fresh bounce fixture already has a handover")
			}
			handovers[name] = string(body)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("pre-bounce handover %s: %w", name, err)
		}
	}

	if err := s.signalStop(8 * time.Second); err != nil {
		return fmt.Errorf("normal drain failed: %w", err)
	}
	// Only the replacement process may supply post-bounce launch evidence.
	drainedLogs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(drainedLogs, preLogs) {
		return fmt.Errorf("daemon log changed during drain")
	}
	normalDrain := false
	for _, line := range strings.Split(string(drainedLogs[len(preLogs):]), "\n") {
		fields, err := queueJourneyLogFields(line)
		if err == nil && fields["msg"] == "shutting down" && fields["exit_mode"] == "normal" && fields["stop_agents"] == "true" {
			normalDrain = true
		}
	}
	if !normalDrain {
		return fmt.Errorf("drain lacks normal stop-agents evidence; upgrade exit is not this journey")
	}
	preLogs = drainedLogs
	if err := s.startDaemon(); err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	challenge := "bounce-now-" + uuid.NewString()
	prompt := "What journey continuity secret did I give you before the restart? Do not use tools or search. Reply with exactly two words separated by one space: the saved secret, then " + challenge + ". No labels or punctuation."
	expected := secret + " " + challenge
	directErr := direct(prompt, expected)
	// Check identity even when the provider call failed. A timeout must not
	// disguise an observed session replacement as an external outage.
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("snapshot after bounce: %w", err)
	}
	for name, agent := range before {
		if next, ok := after[name]; !ok || next.SessionID != agent.SessionID || next.Provider != agent.Provider {
			return fmt.Errorf("bounce changed existing session/provider for %s", name)
		}
	}
	if directErr != nil {
		return fmt.Errorf("post-bounce aside turn: %w", directErr)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(logs, preLogs) {
		return fmt.Errorf("daemon launch log was replaced during bounce")
	}
	if err := queueJourneyProvider(logs[len(preLogs):], id, string(s.provider)); err != nil {
		return fmt.Errorf("replacement aside: %w", err)
	}

	for name := range before {
		body, err := os.ReadFile(s.handoverPath(name))
		previous, existed := handovers[name]
		if existed {
			// An older pending migration may legitimately advance or be reaped.
			// Report that residue; this fixture requires no handover record for
			// its fresh aside and no new records for the other seats. File
			// absence alone does not prove that no seed was ever delivered.
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("post-bounce handover %s: %w", name, err)
			}
			if err != nil || string(body) != previous {
				fmt.Printf("J14 pre-existing handover changed for %s; prior migration is outside this drain-only probe\n", name)
			}
			continue
		}
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("post-bounce handover %s: %w", name, err)
		}
		return fmt.Errorf("bounce wrote a new T285 handover for %s", name)
	}
	if _, err := s.mcpText("jevons_thread_remove", map[string]any{"id": id}); err != nil {
		return fmt.Errorf("remove bounce fixture: %w", err)
	}
	list, err := s.mcpText("jevons_thread_list", nil)
	if err != nil {
		return err
	}
	if strings.Contains(list, id) {
		return fmt.Errorf("bounce fixture remains in thread list")
	}
	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if agent.Name == id {
			return fmt.Errorf("bounce fixture remains in agent registry")
		}
	}
	return nil
}

// completeBounceOwnerTurn is the 🎯T627.1 main-conversation half of J14:
// the isolate overseer must finish a fresh owner request before drain.
//
// 🎯T834: a reply that misses the bounded wait fails with the causes the
// isolate recorded — busy notice, other backend, silent provider, down
// overseer — rather than a bare deadline. The wait is not lengthened.
func (s *suite) completeBounceOwnerTurn(logStart int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	conn, raw, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		if outage := asOutage("bounce owner mux", err); outage != nil {
			return outage
		}
		return fmt.Errorf("bounce owner mux: %w", err)
	}
	defer conn.CloseNow()
	watch := &ownerStallWatch{}
	frames := watch.tap(ctx, raw)
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return fmt.Errorf("bounce owner replay: %w", err)
	}
	token := "bounce-main-" + uuid.NewString()
	prompt := "Reply with exactly: " + token + ". Do not use tools."
	phases, _, _, _ := watch.snapshot()
	var atSend ownerStallPhase
	if len(phases) > 0 {
		atSend = phases[len(phases)-1]
	}
	sentAt := time.Now()
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": prompt}); err != nil {
		return fmt.Errorf("bounce owner send: %w", err)
	}
	if err := waitOwnerMuxReplyMatching(ctx, frames, prompt, token, func(text string) bool {
		return bounceDirectMatches(text, token)
	}, watch.observe); err != nil {
		if outage := asOutage("bounce owner turn", err); outage != nil {
			return outage
		}
		if isOutage(err) {
			return err
		}
		return fmt.Errorf("pre-bounce owner turn: %w — %s", err, s.ownerStallDiagnosis(watch, atSend, sentAt, logStart, err))
	}
	return nil
}

// ownerStallDiagnosis gathers the isolate's own record of the stalled owner
// turn — registry, daemon log since logStart, mux observations — for
// diagnoseOwnerStall. A read failure is named in the diagnosis, not hidden.
func (s *suite) ownerStallDiagnosis(watch *ownerStallWatch, atSend ownerStallPhase, sentAt time.Time, logStart int64, waitErr error) string {
	phases, assistant, ended, lastEnded := watch.snapshot()
	ev := ownerStallEvidence{
		Requested: string(s.provider), SentAt: sentAt, AtSend: atSend, Phases: phases,
		EchoIndex: ownerEchoIndex(waitErr), Assistant: assistant, Ended: ended, LastEnded: lastEnded,
	}
	var readErrs []string
	if reg, err := bounceRegistrySnapshot(s.agentsPath()); err != nil {
		readErrs = append(readErrs, "registry unread: "+err.Error())
	} else {
		ev.OverseerProvider = string(reg[overseerName].Provider)
	}
	if logs, err := os.ReadFile(s.logPath); err != nil {
		readErrs = append(readErrs, "daemon log unread: "+err.Error())
	} else if logStart <= int64(len(logs)) {
		ev.Logs = logs[logStart:]
	} else {
		ev.Logs = logs
	}
	out := diagnoseOwnerStall(ev)
	if len(readErrs) > 0 {
		out += "; " + strings.Join(readErrs, "; ")
	}
	return out
}

// ownerEchoIndex recovers the owner echo index from the reply wait's error;
// the wait reports it as "(owner index=N)".
func ownerEchoIndex(err error) int {
	var index int
	if err == nil {
		return 0
	}
	msg := err.Error()
	if i := strings.Index(msg, "owner index="); i >= 0 {
		_, _ = fmt.Sscanf(msg[i:], "owner index=%d", &index)
	}
	return index
}

// logSize is the daemon log's current length: a start offset for reading
// what the isolate logged afterwards. 0 when the log cannot be read.
func (s *suite) logSize() int64 {
	info, err := os.Stat(s.logPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

func bounceDirectMatches(out, expected string) bool {
	got := strings.TrimSpace(out)
	if got == expected {
		return true
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}

// jSwitchSeedShape is the 🎯T285.1 isolate oracle: a provider switch
// delivers a brief-shaped seed, not a walk-the-predecessor-file assignment.
// When Distill is too thin the work session id differs from the compact id.
