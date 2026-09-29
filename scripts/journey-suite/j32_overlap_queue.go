// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// jOverseerOverlapQueue proves the owner can send again while a real provider
// turn is active and the message is never lost. Since 🎯T903 the second
// message is steered into the running owner turn under the owner ladder; on
// a seat that cannot steer it remains in Jevons's durable queue instead (OMP
// acknowledges a socket write before it can reject an overlapping prompt).
// Either way the overseer answers it.
func (s *suite) jOverseerOverlapQueue() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*turnTimeout)
	defer cancel()
	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return err
	}

	ready := filepath.Join(s.stateDir, "overlap-ready")
	release := filepath.Join(s.stateDir, "overlap-release")
	defer func() {
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "J32 release held turn: %v\n", err)
		}
	}()
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	const holdLimitSeconds = 90
	command := fmt.Sprintf("printf 'ready\\n' > %s; n=0; while [ ! -f %s ] && [ \"$n\" -lt %d ]; do sleep 1; n=$((n+1)); done; test -f %s",
		quote(ready), quote(release), holdLimitSeconds, quote(release))
	firstToken := "overlap-first-" + uuid.NewString()
	first := "Use Bash to run exactly this command and wait for it to return. Do not create the release file, background the command, or finish early:\n" + command + "\nThen reply with exactly: " + firstToken
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": first}); err != nil {
		return err
	}
	deadline := time.Now().Add(turnTimeout)
	for {
		if body, err := os.ReadFile(ready); err == nil && string(body) == "ready\n" {
			break
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("first owner turn never established its live Bash hold")
		}
		select {
		case data := <-frames:
			if _, _, err := decodeOwnerMux(data); err != nil {
				return err
			}
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	secondToken := "overlap-second-" + uuid.NewString()
	second := "Reply with exactly: " + secondToken + ". Do not use tools."
	escalatedBefore := ownerEscalations(s.logPath)
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": second}); err != nil {
		return err
	}
	// Do not release the first turn until the product itself has taken the
	// second message: steered into the running owner turn under the owner
	// ladder (🎯T903), or — on a seat that cannot steer — retained in the
	// durable owner queue. A provider-side hidden queue is not this contract.
	queuePath := filepath.Join(s.stateDir, "owner_queue.json")
	deadline = time.Now().Add(10 * time.Second)
	for {
		if raw, err := os.ReadFile(queuePath); err == nil && strings.Contains(string(raw), secondToken) {
			break
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if ownerEscalations(s.logPath) > escalatedBefore {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("second owner message was neither steered nor retained while the first turn was active")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		return err
	}
	if err := waitOwnerMuxReply(ctx, frames, second, secondToken); err != nil {
		return fmt.Errorf("second owner message: %w", err)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	return queueJourneyProvider(logs, overseerName, string(s.provider))
}

// ownerEscalations counts the daemon's owner-urgency escalating sends
// (🎯T899 / 🎯T903) logged so far.
func ownerEscalations(logPath string) int {
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), "outcome=escalating class=owner")
}
