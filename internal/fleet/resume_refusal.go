// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// DefaultResumeRefusalEscalateAfter matches the cost enforcer resume cap.
const DefaultResumeRefusalEscalateAfter = 3

// ErrResumeRefusalExhausted means fresh-session escalation still could not start.
var ErrResumeRefusalExhausted = errors.New("conversation resume refused after bounded attempts")

// ErrSeatReapedForResumeRefusal reports the row was removed for frontier respawn.
var ErrSeatReapedForResumeRefusal = errors.New("seat reaped after resume refusal escalation")

// IsConversationResumeRefused reports broker-side conversation resume refusal.
func IsConversationResumeRefused(err error) bool {
	if err == nil {
		return false
	}
	if claudia.IsCursorResumeDenied(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "existing conversation required") {
		return true
	}
	if strings.Contains(msg, "refusing to mint a replacement session") {
		return true
	}
	if strings.Contains(msg, "loaded in the sidecar under another session") {
		return true
	}
	return false
}

// AutoStartSeat is boot reattach with T1135 escalation on resume refusal.
func AutoStartSeat(ctx context.Context, reg *claudia.Registry, name string) (*claudia.Agent, error) {
	if reg == nil {
		return nil, fmt.Errorf("auto-start %q: no agent registry", name)
	}
	var agent *claudia.Agent
	var err error
	if contextual, ok := any(reg).(interface {
		AdoptOrLaunchContext(context.Context, string) (*claudia.Agent, error)
	}); ok {
		agent, err = contextual.AdoptOrLaunchContext(ctx, name)
	} else {
		agent, err = reg.AdoptOrLaunch(name)
	}
	if err == nil {
		clearResumeRefusal(name)
		return agent, nil
	}
	if !IsConversationResumeRefused(err) {
		return nil, err
	}
	return launchAutoRecoveringAfterRefusal(reg, name)
}

// LaunchAutoRecovering is unattended relaunch with T1135 escalation.
func LaunchAutoRecovering(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	agent, err := LaunchRecovering(reg, name)
	if err == nil {
		clearResumeRefusal(name)
		return agent, nil
	}
	if !IsConversationResumeRefused(err) {
		return nil, err
	}
	return launchAutoRecoveringAfterRefusal(reg, name)
}

// ResumeRefusalAttempts is how many unattended resume refusals are recorded
// for name (🎯T1135). The fleet-health sweep retries stopped AutoStart seats
// only while this is non-zero so intent flips alone do not relaunch rows.
func ResumeRefusalAttempts(name string) int {
	resumeRefusalMu.Lock()
	n := resumeRefusalCounts[name]
	resumeRefusalMu.Unlock()
	return n
}

func launchAutoRecoveringAfterRefusal(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	attempts := noteResumeRefusalFailure(name)
	if attempts < resumeRefusalEscalateAfter() {
		return nil, fmt.Errorf("resume refused (attempt %d/%d)", attempts, resumeRefusalEscalateAfter())
	}
	lost, err := forceFreshSession(reg, name)
	if err != nil {
		return nil, fmt.Errorf("%w: fresh session: %v", ErrResumeRefusalExhausted, err)
	}
	agent, err := LaunchReconciled(reg, name)
	if err == nil {
		clearResumeRefusal(name)
		slog.Warn("resume refusal escalated to fresh session",
			"name", name, "attempts", attempts, "detail", lost.Describe())
		return agent, nil
	}
	if IsConversationResumeRefused(err) {
		if rerr := ReapResumeRefusedSeat(reg, name); rerr != nil {
			return nil, fmt.Errorf("%w: reap failed: %v", ErrResumeRefusalExhausted, rerr)
		}
		clearResumeRefusal(name)
		slog.Warn("resume refusal escalated to reap for frontier respawn",
			"name", name, "attempts", attempts, "last_err", err)
		return nil, ErrSeatReapedForResumeRefusal
	}
	return nil, err
}

func forceFreshSession(reg *claudia.Registry, name string) (LostSession, error) {
	def := reg.Def(name)
	if def == nil {
		return LostSession{}, fmt.Errorf("no agent %q", name)
	}
	return rotateOntoFreshSession(reg, def)
}

// ReapResumeRefusedSeat removes the row so frontier can respawn (T1096/T1135).
func ReapResumeRefusedSeat(reg *claudia.Registry, name string) error {
	_, err := (&fleetlog.Account{}).Remove(reg, name, fleetlog.Removal{
		Reason: fleetlog.ReasonResumeRefusal,
		Detail: "conversation could not be resumed after bounded attempts (T1135); frontier may respawn",
	})
	return err
}

// ConversationMissingDisplay is the fleet-row reason for a seat whose stored
// conversation cannot be resumed (🎯T1100). Inspect, kill/deregister, or a
// deliberate fresh session are the fixes — not Sign in.
const ConversationMissingDisplay = "stored conversation is missing for this seat — inspect the row, kill or deregister the seat, or start on a fresh session when you accept losing the old thread"

// RecordedConversationResumeRefusal reports whether name's last rehydrate
// failure was a broker conversation-resume refusal (🎯T1100).
func RecordedConversationResumeRefusal(name string) bool {
	v, ok := rehydrateFailures.Load(name)
	if !ok {
		return false
	}
	rec, ok := v.(rehydrateRecord)
	return ok && rec.Text != "" && IsConversationResumeRefused(errors.New(rec.Text))
}

// RehydrateAuthRecoverable reports a stopped seat whose recorded failure is a
// plan-login loss the owner can repair through Sign in (🎯T1100). A missing
// conversation is broken but not auth-recoverable.
func RehydrateAuthRecoverable(def claudia.AgentDef) bool {
	v, ok := rehydrateFailures.Load(def.Name)
	if !ok {
		return false
	}
	rec, ok := v.(rehydrateRecord)
	if !ok || rec.Text == "" || IsConversationResumeRefused(errors.New(rec.Text)) {
		return false
	}
	return recordedPlanAuthFailure(v)
}

// RehydrateBrokenDisplayText rewrites a stored launch refusal for the cockpit.
func RehydrateBrokenDisplayText(raw string) string {
	if IsConversationResumeRefused(errors.New(raw)) {
		return ConversationMissingDisplay
	}
	return raw
}
