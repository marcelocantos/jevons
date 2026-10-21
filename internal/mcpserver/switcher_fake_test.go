// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"sync"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/thread"
)

// fakeSwitcher stands in for *fleet.Claudia's in-place switch. It records
// every switch and launch; on success it rewrites the seat's row in reg (when
// set) as the real switch does, keeping the session.
type fakeSwitcher struct {
	mu        sync.Mutex
	reg       *claudia.Registry
	live      bool
	err       error
	launchErr error
	switches  []string // "name→provider/model force=…"
	launched  []string
}

func (f *fakeSwitcher) SwitchModel(name string, to claudia.Provider, model string, force bool) (claudia.SeatSwitch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := name + "→" + string(to) + "/" + model
	if force {
		rec += " force"
	}
	f.switches = append(f.switches, rec)
	if f.err != nil {
		return claudia.SeatSwitch{}, f.err
	}
	out := claudia.SeatSwitch{Live: f.live, Changed: true, To: to, Model: model}
	if f.reg != nil {
		if def := f.reg.Def(name); def != nil {
			out.From, out.FromModel = def.Provider, def.Model
			next := *def
			if strings.TrimSpace(string(to)) != "" {
				next.Provider = seatprovider.Resolve(to).Launch
				out.To = next.Provider
			}
			next.Model = model
			_ = f.reg.Register(next)
		}
	}
	return out, nil
}

func (f *fakeSwitcher) Launch(t *thread.Thread) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.launchErr != nil {
		return f.launchErr
	}
	f.launched = append(f.launched, t.ID)
	return nil
}

func (f *fakeSwitcher) calls() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.switches...), append([]string(nil), f.launched...)
}
