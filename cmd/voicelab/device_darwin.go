// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package main

import "github.com/marcelocantos/jevons/internal/voicelab"

// newAudioDevice opens the macOS VoiceProcessingIO audio unit — same
// AEC + NS + AGC stack FaceTime uses, so speakers don't echo back
// through the mic. malgo stays available as a fallback if ever needed
// but isn't wired here because every voicelab user is on macOS.
func newAudioDevice() (audioDevice, error) {
	return voicelab.NewVoiceProcDevice()
}
