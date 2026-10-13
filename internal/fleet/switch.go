// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
)

// ModelSwitchHow names how a landed model change was made. Stable for the
// event journal: a diagnosis greps it rather than reconstructing the seat
// from memory.
const ModelSwitchHow = "switch"

// ModelSwitch is one change of the model a seat is recorded as running.
// From/To are model ids. Provider is the provider after the change;
// FromProvider is the one before it.
type ModelSwitch struct {
	Name         string
	Provider     string
	FromProvider string
	From         string
	To           string
	How          string
}

// noteModelSwitch delivers sw to the host hook. A same-model, same-provider
// note is dropped so a rebind that changed nothing is not a switch.
func (f *Claudia) noteModelSwitch(sw *ModelSwitch) {
	if f == nil || sw == nil || f.onModelSwitch == nil {
		return
	}
	if strings.TrimSpace(sw.From) == strings.TrimSpace(sw.To) &&
		strings.TrimSpace(sw.FromProvider) == strings.TrimSpace(sw.Provider) {
		return
	}
	f.onModelSwitch(sw)
}

// SwitchModel moves an agent to provider `to` and model, in place, keeping
// its name, session and conversation (claudia 🎯T195). It is the one way a
// seat changes model: nothing is summarised, no successor seat is started,
// and the chat, queue and reports attached to the seat stay attached.
//
// An empty provider keeps the seat's own; an empty model takes the
// destination plan's standard session model. A running seat switches
// between turns: a turn in flight refuses the switch unless force is set,
// which interrupts it first. A stopped seat has its definition rewritten,
// so its next launch resumes the conversation on the new model.
func (f *Claudia) SwitchModel(name string, to claudia.Provider, model string, force bool) (claudia.SeatSwitch, error) {
	if f == nil || f.reg == nil {
		return claudia.SeatSwitch{}, fmt.Errorf("switch model: no agent registry")
	}
	def := f.reg.Def(name)
	if def == nil {
		return claudia.SeatSwitch{}, fmt.Errorf("switch model: no agent %q", name)
	}
	target := def.Provider
	if p := strings.TrimSpace(string(to)); p != "" {
		target = claudia.SubscriptionSeatProvider(claudia.Provider(p))
	}
	// 🎯T763: refuse before anything changes when the move would have to
	// drop a restriction the destination cannot enforce.
	if err := providerSwitchRefusal(*def, target); err != nil {
		return claudia.SeatSwitch{}, fmt.Errorf("switch %q: %w", name, err)
	}
	var sw claudia.SeatSwitch
	var err error
	if f.switchModel != nil {
		sw, err = f.switchModel(name, target, strings.TrimSpace(model), force)
	} else {
		sw, err = f.reg.SwitchModel(context.Background(), name, target, strings.TrimSpace(model), force)
	}
	if err != nil {
		return sw, fmt.Errorf("switch %q: %w", name, err)
	}
	if !sw.Changed {
		return sw, nil
	}
	// T1032: the destination may not honour every provider-native setting
	// the seat carried; drop those on the row, recorded, so the next launch
	// is not refused over them.
	if err := reconcileAfterSwitch(f.reg, name, sw.From); err != nil {
		return sw, fmt.Errorf("switch %q: reconcile destination: %w", name, err)
	}
	f.noteModelSwitch(&ModelSwitch{
		Name: name, Provider: string(sw.To), FromProvider: string(sw.From),
		From: sw.FromModel, To: sw.Model, How: ModelSwitchHow,
	})
	f.logEvent("fleet_switch", "switched", map[string]any{
		"name": name, "to": string(sw.To), "from_provider": string(sw.From),
		"from_model": sw.FromModel, "to_model": sw.Model, "live": sw.Live,
	})
	slog.Info("agent switched model in place", "name", name, "live", sw.Live,
		"from", string(sw.From)+"/"+sw.FromModel, "to", string(sw.To)+"/"+sw.Model)
	return sw, nil
}

// PinModel switches a fleet agent's model on its own provider (🎯T285.2,
// 🎯T617). It is [Claudia.SwitchModel] with the provider left alone; a
// seat already on the model is a no-op, not an error.
func (f *Claudia) PinModel(name, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("pin model %q: model is required", name)
	}
	if _, err := f.SwitchModel(name, "", model, false); err != nil {
		return fmt.Errorf("pin model: %w", err)
	}
	return nil
}
