// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
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
	f.reg.Stop(name)
	if err := f.Launch(&thread.Thread{ID: name, Model: model}); err != nil {
		return fmt.Errorf("pin model %q: relaunch: %w", name, err)
	}
	return nil
}
