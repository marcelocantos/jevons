// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"

	"fmt"
	"github.com/marcelocantos/claudia"
	"strings"

	"github.com/marcelocantos/jevons/internal/thread"
)

// PinModel switches a fleet agent's model WITHOUT changing provider
// (🎯T285.2): stop the process, relaunch the SAME session with the new
// pin. This is not a migration — the session store is per-provider, so
// the conversation resumes; nothing is rotated and no handover is
// gathered. Launch's ensureRegistered writes the pin onto the registry
// row before the process comes up, which is the same path an explicit
// spawn-time pin takes (🎯T324).
func (f *Claudia) PinModel(name, model string) error {
	if f == nil || f.reg == nil {
		return fmt.Errorf("pin model: no agent registry")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("pin model %q: model is required", name)
	}
	def := f.reg.Def(name)
	if def == nil {
		return fmt.Errorf("pin model: no agent %q", name)
	}
	if strings.TrimSpace(def.Model) == model {
		return nil // already pinned — a no-op, not an error
	}

	// 🎯T617: switch the live agent when the provider can do it, which is
	// why claudia grew SetModel and CapabilityModelSwitch in the first
	// place. This path never stops the seat and never reloads a session.
	//
	// The old behaviour was unconditional stop-then-relaunch, and every
	// symptom of a failed switch came from it: the relaunch had to
	// re-attach to the existing conversation, so a seat whose session id
	// would not load ("acp session/load …: Path not found") could not be
	// switched at all — and because Stop ran first with no restore, the
	// attempt left a healthy seat stopped and the registry claiming a model
	// nothing was running.
	//
	// SetModel does its own guarding (capability, ready, alive, no turn in
	// flight) and reports an unsupported provider as *claudia.CapabilityError,
	// so trying it first costs nothing and tells us precisely when to fall
	// back rather than making us predict it.
	if live := f.reg.Get(name); live != nil {
		switch err := live.SetModel(model); {
		case err == nil:
			// Persist the new model without disturbing the conversation:
			// Register is an upsert, and it only resets resume state when
			// the SessionID changes, which this copy leaves alone.
			switched := *def
			switched.Model = model
			if err := f.reg.Register(switched); err != nil {
				return fmt.Errorf("pin model %q: record switched model: %w", name, err)
			}
			return nil
		case isUnsupportedCapability(err):
			// Fall through to relaunch: this provider genuinely cannot
			// switch a live session.
		default:
			// A supported switch that failed is a real failure. Do not
			// "recover" by stopping the seat and hoping a relaunch works —
			// that is the behaviour this target exists to remove.
			return fmt.Errorf("pin model %q: %w", name, err)
		}
	}

	// Fallback for providers without the capability. Stop is destructive,
	// so it is only reached when there is no other way, and a failed launch
	// must not leave the seat worse than it found it.
	prev := *def
	f.reg.Stop(name)
	if err := f.Launch(&thread.Thread{ID: name, Model: model}); err != nil {
		if _, relaunchErr := f.reg.Launch(prev.Name); relaunchErr != nil {
			return fmt.Errorf("pin model %q: relaunch: %w (and restoring the seat failed: %v)",
				name, err, relaunchErr)
		}
		return fmt.Errorf("pin model %q: relaunch: %w (seat restored on %s)", name, err, prev.Model)
	}
	return nil
}

// isUnsupportedCapability reports whether err is claudia declining because
// the provider cannot do the thing, as opposed to trying and failing.
func isUnsupportedCapability(err error) bool {
	var capErr *claudia.CapabilityError
	return errors.As(err, &capErr)
}
