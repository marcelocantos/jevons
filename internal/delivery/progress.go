// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package delivery

import "github.com/marcelocantos/claudia"

// Progress types Claudia's escalation ladder emits (claudia 🎯T138),
// now published as claudia.ProgressDeliveryAbsorbed and
// claudia.ProgressDeliveryEscalated (🎯T1008). Aliased here so the
// phase mapper and the mid-turn relay keep one import path.
const (
	ProgressDeliveryAbsorbed  = claudia.ProgressDeliveryAbsorbed
	ProgressDeliveryEscalated = claudia.ProgressDeliveryEscalated
)
