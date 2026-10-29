// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"sync"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// 🎯T845: a seat whose turns end in provider request errors is failed, not
// running/idle; migrate/mint refuses models the account already proved it
// cannot use.

const (
	providerRequestFailureStrikes = 1
	overseerProviderFailureKind   = "overseer-provider-failure"
	overseerProviderFailureSubj   = "overseer"
)

type blockedDestModels struct {
	mu sync.Mutex
	m  map[string]string // provider/model → provider message
}

func (b *blockedDestModels) set(provider, model, msg string) {
	if b == nil {
		return
	}
	key := blockedDestKey(provider, model)
	if key == "" || strings.TrimSpace(msg) == "" {
		return
	}
	b.mu.Lock()
	if b.m == nil {
		b.m = make(map[string]string)
	}
	b.m[key] = strings.TrimSpace(msg)
	b.mu.Unlock()
}

func (b *blockedDestModels) get(provider, model string) string {
	if b == nil {
		return ""
	}
	key := blockedDestKey(provider, model)
	b.mu.Lock()
	msg := b.m[key]
	b.mu.Unlock()
	return msg
}

func blockedDestKey(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return ""
	}
	return strings.ToLower(provider) + "/" + model
}

func modelNotSupported(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "model is not supported") ||
		strings.Contains(msg, "model not supported") ||
		strings.Contains(msg, "not supported when using codex")
}

func providerRequestFailureWorthy(errored bool, class agenterr.Class, reason, terminal string) bool {
	if !errored {
		return false
	}
	if class.IsFailure() && !agenterr.TransientBackend(class) {
		return true
	}
	combined := reason + " " + terminal
	return modelNotSupported(combined)
}

// applyProviderRequestTurnFailure updates idle activity and blocked destinations
// after a terminal turn. Returns true when the seat newly latched failed.
func (s *Server) applyProviderRequestTurnFailure(name string, errored bool, class agenterr.Class, reason, terminal string) bool {
	if s == nil || name == "" {
		return false
	}
	if !providerRequestFailureWorthy(errored, class, reason, terminal) {
		s.clearProviderRequestStreak(name)
		return false
	}
	last := strings.TrimSpace(reason)
	if last == "" {
		last = strings.TrimSpace(terminal)
	}
	if last == "" {
		last = agenterr.OwnerCopy(class, "")
	}
	s.mu.Lock()
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker == nil {
		return false
	}
	tracker.mu.Lock()
	if tracker.by == nil {
		tracker.by = make(map[string]IdleActivity)
	}
	prev := tracker.by[name]
	prev.ProviderErrorStreak++
	prev.LastProviderError = truncate(last, 400)
	if class.IsFailure() {
		prev.FailureClass = class
	}
	prev.TurnErrored = true
	newly := false
	if prev.ProviderErrorStreak >= providerRequestFailureStrikes && !prev.ProviderRequestFailed {
		prev.ProviderRequestFailed = true
		newly = true
	}
	tracker.by[name] = prev
	tracker.mu.Unlock()

	if newly && s.registry != nil {
		if def := s.registry.Def(name); def != nil {
			launch := seatprovider.Resolve(def.Provider).Launch
			s.blockedDest.set(string(launch), def.Model, last)
		}
	}
	if newly {
		s.maybeNotifyOverseerProviderFailure(name, class, last)
	}
	if modelNotSupported(reason + " " + terminal) || modelNotSupported(last) {
		s.latchUnsupportedModelPin(name, last)
	}
	return newly
}

func (s *Server) clearProviderRequestStreak(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	if act, ok := tracker.by[name]; ok {
		act.ProviderErrorStreak = 0
		tracker.by[name] = act
	}
	tracker.mu.Unlock()
}

// clearProviderRequestFailed drops the seat-local failed latch after an
// explicit owner/PO intent clearance (🎯T1052 recovery via jevons_fleet_intent).
func (s *Server) clearProviderRequestFailed(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	if act, ok := tracker.by[name]; ok && act.ProviderRequestFailed {
		act.ProviderRequestFailed = false
		act.ProviderErrorStreak = 0
		act.TurnErrored = false
		tracker.by[name] = act
	}
	tracker.mu.Unlock()
}

// ProviderRequestFailure reports whether name is latched failed on provider
// request errors and the fields for /api/agents.
func (s *Server) ProviderRequestFailure(name string) (class agenterr.Class, lastError string, failed bool) {
	if s == nil || name == "" {
		return agenterr.ClassNone, "", false
	}
	s.mu.Lock()
	tracker := s.idleActivity
	s.mu.Unlock()
	if tracker == nil {
		return agenterr.ClassNone, "", false
	}
	act := tracker.Get(name)
	if !act.ProviderRequestFailed {
		return agenterr.ClassNone, "", false
	}
	return act.FailureClass, act.LastProviderError, true
}

// ValidateDestinationModel refuses mint/migrate to a model already proved
// unusable (blockedDest). Non-explicit pins are also checked against the
// linked Codex catalog; explicit caller pins win even when the catalog omits
// the slug (🎯T1046).
func (s *Server) ValidateDestinationModel(provider, model string, explicitPin bool) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	launch := seatprovider.Resolve(claudia.Provider(provider)).Launch
	if msg := s.blockedDest.get(string(launch), model); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	if explicitPin {
		return nil
	}
	if seatprovider.Resolve(claudia.Provider(provider)).Plan == seatprovider.Codex && !s.codexCatalogHasModel(model) {
		return fmt.Errorf("model %q is not available on the linked Codex account", model)
	}
	return nil
}

func (s *Server) deliverRefuseIfProviderRequestFailed(name string) error {
	class, msg, failed := s.ProviderRequestFailure(name)
	if !failed {
		return nil
	}
	if msg == "" {
		msg = agenterr.OwnerCopy(class, "provider refused the turn")
	}
	return fmt.Errorf("agent %q is failed (%s): %s", name, class.String(), msg)
}

func (s *Server) maybeNotifyOverseerProviderFailure(name string, class agenterr.Class, detail string) {
	if s == nil || name == "" || !strings.EqualFold(name, s.overseerName()) {
		return
	}
	n := s.ownerNotifier
	if n == nil {
		return
	}
	text := fmt.Sprintf(
		"Overseer %q cannot turn: provider request failed (%s). %s "+
			"No fleet seat can mint until the overseer is on a supported model — switch it from the fleet tree.",
		name, class.String(), truncate(strings.TrimSpace(detail), 320))
	n.NotifyOwner(overseerProviderFailureSubj, overseerProviderFailureKind, text)
}

// FormatProviderRequestFailureLine is the agent_list decoration (🎯T845).
func FormatProviderRequestFailureLine(class agenterr.Class, lastError string) string {
	lastError = strings.TrimSpace(lastError)
	if lastError == "" {
		lastError = "provider request error"
	}
	return fmt.Sprintf("provider request failed (%s): %s", class.String(), lastError)
}
