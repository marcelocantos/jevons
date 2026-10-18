// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package agenterr classifies agent send/delivery errors without assuming
// a single provider's wire strings (🎯T214 residual J6).
package agenterr

// IsPromptBusy reports whether err is a typed concurrent prompt, turn, or
// task. The caller queues or interrupts. A rendered sentence is not busy.
func IsPromptBusy(err error) bool {
	switch ProviderCause(err) {
	case CauseTurnInFlight, CauseBusy:
		return true
	default:
		return false
	}
}
