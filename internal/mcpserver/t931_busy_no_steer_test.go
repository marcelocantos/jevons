// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// relayedSteerRefusal is the 2026-09-29 refusal, verbatim as the broker
// relayed it to the daemon: a string, which errors.Is cannot match.
var relayedSteerRefusal = errors.New("broker protocol: agent_failed: steer unsupported: claude provider: provider has no steer mechanism (policy queue_until_idle)")

// noSteerSeat is a busy Claude Code seat that cannot say what it can do: it
// runs a ladder in principle, but a steer is refused the way the broker
// relays it.
type noSteerSeat struct {
	fakeSender
	ladders []escalate.Ladder
}

func (f *noSteerSeat) base() *noSteerSeat { return f }

func (f *noSteerSeat) TurnPhase() claudia.TurnPhase {
	if f.inFlight {
		return claudia.TurnInTurn
	}
	return claudia.TurnIdle
}

func (f *noSteerSeat) SendEscalating(_ string, esc escalate.Ladder) (claudia.DeliveryOutcome, error) {
	f.ladders = append(f.ladders, esc)
	return claudia.DeliveryOutcome{}, relayedSteerRefusal
}

// capsNoSteerSeat adds the capability report a broker-held claudia.Agent gives.
type capsNoSteerSeat struct{ noSteerSeat }

func (f *capsNoSteerSeat) TurnCaps() claudia.TurnCaps {
	return claudia.ProviderTurnCaps(claudia.ProviderClaude)
}

// 🎯T931: a plain owner send to a busy seat that cannot steer is queued for
// the next turn boundary and answers "queued" — not 502 client_bug — and the
// daemon delivers it at the next terminal stop.
func TestT931PlainSendToBusyNonSteerableSeatQueuesThenDelivers(t *testing.T) {
	for name, seat := range map[string]interface {
		agentSender
		base() *noSteerSeat
	}{
		"seat reports caps": &capsNoSteerSeat{noSteerSeat{fakeSender: fakeSender{alive: true, inFlight: true}}},
		"seat cannot say":   &noSteerSeat{fakeSender: fakeSender{alive: true, inFlight: true}},
	} {
		t.Run(name, func(t *testing.T) {
			const worker = "jv-t926-live-verify"
			const payload = "wind up: commit what you have and report"
			s, _ := chainServer(t, nil)
			s.stateDir = t.TempDir()
			s.SetSenderResolver(func(n string) (agentSender, bool, error) {
				if n != worker {
					return nil, false, errors.New("unknown")
				}
				return seat, false, nil
			})
			s.noteTurnInFlight(worker)

			res, err := s.DeliverAgentMessageMode(worker, payload, OriginOwner, delivery.ModeSubmit)
			if err != nil {
				t.Fatalf("plain owner send to a busy non-steerable seat: %v", err)
			}
			if res.Status != "queued" {
				t.Fatalf("status=%q want queued (message=%q)", res.Status, res.Message)
			}
			b := seat.base()
			if _, reports := seat.(*capsNoSteerSeat); reports && len(b.ladders) != 0 {
				t.Fatalf("a steer ladder was offered to a seat that reported it cannot steer: %+v", b.ladders)
			}
			if len(b.sent) != 0 {
				t.Fatalf("text offered to the busy process: %v", b.sent)
			}
			if n := s.pendingAgentSends(worker); n != 1 {
				t.Fatalf("daemon queue depth=%d want 1", n)
			}

			// The next terminal stop: the seat goes idle and the queue drains.
			b.inFlight = false
			s.noteTurnEnded(worker)
			s.drainAgentSendQueue(worker)
			if len(b.sent) != 1 || b.sent[0] != payload {
				t.Fatalf("after the terminal stop the seat received %q, want [%q]", b.sent, payload)
			}
			if n := s.pendingAgentSends(worker); n != 0 {
				t.Fatalf("pending=%d after drain, want 0", n)
			}
		})
	}
}
