// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// modeErrSender answers SendMode with a chosen error. It has no request-id
// method, so the send path does not bind a receipt before asking the seat.
type modeErrSender struct {
	err   error
	calls int
}

func (f *modeErrSender) Alive() bool      { return true }
func (f *modeErrSender) Interrupt() error { return nil }
func (f *modeErrSender) Send(string) error {
	f.calls++
	return f.err
}
func (f *modeErrSender) SendModeString(string, string) (string, string, error) {
	f.calls++
	return "", "", f.err
}

// 🎯T766.4 seam: the steer decision holds a typed or relayed
// ErrSteerUnsupported. A rendered mention is a send failure. An idle turn
// is not that refusal: the busy-phrase check may still hold it, and the
// copy must not say claudia refused the mechanism. The call is
// deliverToSenderMode, which is what an owner steer runs.
func TestT766SteerRefusalIsTypedNotRendered(t *testing.T) {
	relayed := errors.New("broker protocol: agent_failed: steer unsupported: claude provider: provider has no steer mechanism (policy queue_until_idle)")
	quoted := errors.New("broker protocol: agent_failed: provider response quoted: steer unsupported: x")
	bare := errors.New("steer unsupported: but not relayed and not the sentinel")
	const (
		typedHold = "typed"
		busyHold  = "busy"
		rejected  = "rejected"
	)
	for _, c := range []struct {
		name string
		err  error
		mode delivery.Mode
		want string
	}{
		{"relayed sentinel", relayed, delivery.ModeSteer, typedHold},
		{"wrapped sentinel", fmt.Errorf("seat: %w", claudia.ErrSteerUnsupported), delivery.ModeSteer, typedHold},
		{"quoted mention", quoted, delivery.ModeSteer, rejected},
		{"bare rendered text", bare, delivery.ModeSteer, rejected},
		{"idle turn", claudia.ErrTurnIdle, delivery.ModeSteer, busyHold},
		{"submit of the same refusal", relayed, delivery.ModeSubmit, rejected},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{}
			proc := &modeErrSender{err: c.err}
			const text = "fold this into the open turn"
			res, err := deliverObservedToSenderMode(s, "po", text, c.mode, proc, false, confirmByCaller)
			rows, serr := s.sendQueue().Snapshot("po")
			if serr != nil {
				t.Fatal(serr)
			}
			if proc.calls != 1 {
				t.Fatalf("seat calls=%d, want the send path to ask the process once", proc.calls)
			}
			typedCopy := strings.Contains(res.Message, "claudia reports this handle cannot steer")
			switch c.want {
			case typedHold:
				if err != nil || res.Status != "queued" || res.Mechanism != delivery.MechanismQueueUntilIdle || !typedCopy {
					t.Fatalf("typed steer refusal: res=%+v err=%v", res, err)
				}
				if len(rows) != 1 || rows[0].Text != text {
					t.Fatalf("queue=%+v", rows)
				}
			case busyHold:
				if err != nil || res.Status != "queued" || typedCopy {
					t.Fatalf("idle turn was classified as a steer refusal: res=%+v err=%v", res, err)
				}
				if !strings.Contains(res.Message, "cannot fold text into its open turn") {
					t.Fatalf("message=%q", res.Message)
				}
				if len(rows) != 1 || rows[0].Text != text {
					t.Fatalf("queue=%+v", rows)
				}
			default:
				if err == nil || res.Status == "queued" || typedCopy || len(rows) != 0 {
					t.Fatalf("rendered or non-steer error was held as a steer refusal: res=%+v err=%v rows=%+v", res, err, rows)
				}
			}
		})
	}
}

// handoffSeat can carry a host request id and an escalation ladder, which is
// what makes an owner nudge claim an urgent handoff.
type handoffSeat struct {
	calls int
}

func (f *handoffSeat) Alive() bool       { return true }
func (f *handoffSeat) Interrupt() error  { return nil }
func (f *handoffSeat) Send(string) error { f.calls++; return nil }
func (f *handoffSeat) SendWithRequestID(string, string) error {
	f.calls++
	return nil
}
func (f *handoffSeat) TurnPhase() claudia.TurnPhase { return claudia.TurnInTurn }
func (f *handoffSeat) SendEscalating(string, escalate.Ladder) (claudia.DeliveryOutcome, error) {
	f.calls++
	return claudia.DeliveryOutcome{}, nil
}
func (f *handoffSeat) SendEscalatingWithRequestID(string, escalate.Ladder, string) (claudia.DeliveryOutcome, error) {
	f.calls++
	return claudia.DeliveryOutcome{}, nil
}

// 🎯T766.4 seam: an urgent nudge that collides with an attempt this process
// already owns returns to the ordinary path because the error is
// sendq.ErrActiveHandoff, and the second text is not admitted.
func TestT766ActiveHandoffConflictReturnsToOrdinaryPath(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)
	const name = "jv-t766-handoff"
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "gen-1", TargetID: "T766",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s.registry = reg
	if _, _, claimed, err := s.sendQueue().AppendAndClaimWithRequestID(name, "first", "owner-1", time.Now()); err != nil || !claimed {
		t.Fatalf("prime active attempt: claimed=%v err=%v", claimed, err)
	}
	proc := &handoffSeat{}
	s.noteTurnInFlight(name)
	res, handled, err := observedEscalateIfBusy(s, name, "second urgent", config.EscalationOwner, "jevons", proc)
	if s.flightState(name) != FlightInFlight {
		t.Fatal("fixture left flight; the conflict branch was not the one under test")
	}
	if err != nil || handled {
		t.Fatalf("typed conflict must return to the ordinary path: handled=%v err=%v res=%+v", handled, err, res)
	}
	if proc.calls != 0 {
		t.Fatalf("conflict reached the seat: calls=%d", proc.calls)
	}
	rows, serr := s.sendQueue().Snapshot(name)
	if serr != nil {
		t.Fatal(serr)
	}
	if len(rows) != 1 || rows[0].Text != "first" {
		t.Fatalf("second text was admitted: %+v", rows)
	}
}
