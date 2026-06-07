// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package main

import "github.com/marcelocantos/jevons/internal/voicelab"

// newAudioDevice falls back to malgo on non-Darwin. No OS-level AEC,
// so continuous mode will echo without headphones. PTT still works.
func newAudioDevice() (audioDevice, error) {
	return voicelab.NewMalgoDevice()
}
