// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// 🎯T657: delivery.Mode threaded through the fleet send path.
//
// The bool entry points (interrupt=true) stay as shims: they are the
// deprecated alias for mode=interrupt and every daemon-internal caller still
// speaks them. The mode reaches deliverToSenderMode either directly or —
// for callers routed through deliverByNameWith, whose file is owned by
// another in-flight slice (🎯T658) — through a per-agent stash the shim
// reads back (stashSendMode / takeSendMode). The stash is scoped to the
// synchronous call that set it and released on return.

// statusSteered is the wire status for text folded into an open turn.
const statusSteered = "steered"

// sendMech is what a send ran as: the owner's intent and the mechanism the
// daemon (or claudia) actually used. It rides the result and the one status
// log line so an operator can tell a steer from a queue that was reported as
// one.
type sendMech struct {
	Mode      delivery.Mode
	Mechanism string
}

// stashSendMode records the mode the next deliverToSenderWith call for name
// should honour, and returns the release that clears it. Scoped to the
// synchronous deliverByNameWith call: the shim reads it inside the same
// stack, and the release runs when that stack unwinds.
func (s *Server) stashSendMode(name string, mode delivery.Mode) func() {
	s.mu.Lock()
	if s.sendModes == nil {
		s.sendModes = map[string]delivery.Mode{}
	}
	prev, had := s.sendModes[name]
	s.sendModes[name] = mode
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		if had {
			s.sendModes[name] = prev
		} else {
			delete(s.sendModes, name)
		}
		s.mu.Unlock()
	}
}

// takeSendMode is the shim's read: a stashed mode wins; otherwise the bool
// alias decides between interrupt and submit.
func (s *Server) takeSendMode(name string, interrupt bool) delivery.Mode {
	s.mu.Lock()
	mode, ok := s.sendModes[name]
	s.mu.Unlock()
	if ok {
		return mode
	}
	if interrupt {
		return delivery.ModeInterrupt
	}
	return delivery.ModeSubmit
}

// deliverByNameMode is deliverByNameAs with the owner's delivery mode named.
// It carries the mode past deliverByNameWith's bool signature via the stash.
func (s *Server) deliverByNameMode(actor, name, text string, origin SendOrigin, mode delivery.Mode, confirm sendConfirmation) (agentSendResult, error) {
	name = strings.TrimSpace(name)
	release := s.stashSendMode(name, mode)
	defer release()
	return s.deliverByNameWith(actor, name, text, origin, mode.Interrupts(), confirm)
}

// sendToAgentMode is the MCP fleet form of sendToAgentAs with a mode.
func (s *Server) sendToAgentMode(actor, name, text string, mode delivery.Mode) (agentSendResult, error) {
	return s.deliverByNameMode(actor, name, text, OriginAgent, mode, confirmHere)
}

// DeliverAgentMessageMode is DeliverAgentMessageAs with the owner's mode
// named; the HTTP send handler and the mux `send` case use it (🎯T657).
func (s *Server) DeliverAgentMessageMode(name, text string, origin SendOrigin, mode delivery.Mode) (AgentDeliverResult, error) {
	res, err := s.deliverByNameMode(ActorOwnerSurface, name, text, origin, mode, confirmHere)
	if err != nil {
		return AgentDeliverResult{}, err
	}
	return AgentDeliverResult{
		Status:    res.Status,
		Message:   res.Message,
		Queued:    res.Queued,
		Mode:      string(res.Mode),
		Mechanism: res.Mechanism,
	}, nil
}

// sendModeOutcome is the pin-free projection of claudia's DeliveryOutcome.
type sendModeOutcome struct {
	Mechanism   string
	PhaseBefore string
}

// sendModeFunc is the seam through which a mode reaches the seat's process.
type sendModeFunc func(text string, mode delivery.Mode) (sendModeOutcome, error)

// modeSender is the pin-free optional interface a sender may implement to
// carry a mode natively (adapters, fakes). claudia.Agent cannot satisfy it —
// its SendMode takes claudia.DeliveryMode — so the reflective probe below is
// the load-bearing path for real seats.
type modeSender interface {
	SendModeString(text, mode string) (mechanism, phaseBefore string, err error)
}

// sendModeSeam returns how to deliver with a mode on this process, or nil
// when the process only knows Send/Interrupt.
//
// 🎯T448: go.mod pins claudia v0.34.0, which predates SendMode / TurnCaps
// (claudia 🎯T72). Naming those symbols would not compile against the pin,
// and an interface assertion cannot be written without naming the parameter
// type. So the probe is reflective: a method called SendMode taking
// (string-kind, string-kind) and returning (struct with string-kind
// Mechanism and PhaseBefore fields, error). The sibling claudia in
// ../go.work matches; the pin does not, and reports the seam as missing.
func sendModeSeam(proc agentSender) sendModeFunc {
	if proc == nil {
		return nil
	}
	if ms, ok := proc.(modeSender); ok {
		return func(text string, mode delivery.Mode) (sendModeOutcome, error) {
			mech, phase, err := ms.SendModeString(text, string(mode))
			return sendModeOutcome{Mechanism: mech, PhaseBefore: phase}, err
		}
	}
	v := reflect.ValueOf(proc)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	m := v.MethodByName("SendMode")
	if !m.IsValid() {
		return nil
	}
	t := m.Type()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if t.NumIn() != 2 || t.In(0).Kind() != reflect.String || t.In(1).Kind() != reflect.String ||
		t.NumOut() != 2 || t.Out(0).Kind() != reflect.Struct || !t.Out(1).Implements(errType) {
		return nil
	}
	mechF, ok1 := t.Out(0).FieldByName("Mechanism")
	phaseF, ok2 := t.Out(0).FieldByName("PhaseBefore")
	if !ok1 || !ok2 || mechF.Type.Kind() != reflect.String || phaseF.Type.Kind() != reflect.String {
		return nil
	}
	return func(text string, mode delivery.Mode) (sendModeOutcome, error) {
		outs := m.Call([]reflect.Value{
			reflect.ValueOf(text).Convert(t.In(0)),
			reflect.ValueOf(string(mode)).Convert(t.In(1)),
		})
		out := sendModeOutcome{
			Mechanism:   outs[0].FieldByIndex(mechF.Index).String(),
			PhaseBefore: outs[0].FieldByIndex(phaseF.Index).String(),
		}
		var err error
		if !outs[1].IsNil() {
			err = outs[1].Interface().(error)
		}
		return out, err
	}
}

var sendModeSeamMissingOnce sync.Once

// logSendModeSeamMissing says once per process that steer was asked of a
// seat whose process has no SendMode. The fallback is honest — the text is
// queued and the mechanism says so — but the owner should learn why ⌘Enter
// behaves like Enter until the claudia pin moves (🎯T448 / 🎯T608).
func logSendModeSeamMissing(name string) {
	sendModeSeamMissingOnce.Do(func() {
		slog.Warn("🎯T657 steer requested but this seat's process has no SendMode; steer is inert until an owner-initiated release bumps the claudia pin (🎯T448 / 🎯T608)",
			"component", "agent_send", "name", name,
			"fallback", "submit when idle, "+delivery.MechanismQueueUntilIdle+" when busy")
	})
}

// isSteerUnsupported recognises claudia's ErrSteerUnsupported by text, the
// pin having no symbol to compare against.
func isSteerUnsupported(err error) bool {
	return err != nil && strings.Contains(err.Error(), "steer unsupported")
}

// describeMode renders the mode for a caller-facing message; empty for the
// default so existing submit messages read unchanged.
func describeMode(mm sendMech) string {
	if mm.Mode == "" || mm.Mode == delivery.ModeSubmit {
		if mm.Mechanism == "" || mm.Mechanism == delivery.MechanismSubmit {
			return ""
		}
	}
	return fmt.Sprintf(" [mode=%s mechanism=%s]", mm.Mode, mm.Mechanism)
}
