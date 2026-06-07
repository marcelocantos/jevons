// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// mixNoise returns a copy of utterancePCM with white noise overlaid at
// the requested RMS amplitude (0.0 = no noise, 1.0 = full-scale RMS,
// which is much louder than any utterance). Useful values for testing
// car-cabin robustness sit in the 0.05–0.2 range — audible interference
// that should still let a good ASR transcribe the speech.
//
// White noise is a stress test; real car noise is mostly low-frequency
// rumble that's easier for ASR to handle. Passing white noise gives
// confidence the real-world case will too.
func mixNoise(utterancePCM []byte, rmsAmplitude float64) []byte {
	if rmsAmplitude <= 0 {
		out := make([]byte, len(utterancePCM))
		copy(out, utterancePCM)
		return out
	}

	// Target RMS in int16 units. PCM16 full scale = 32767, so
	// amplitude 1.0 → RMS 32767. For white-noise sigma = RMS.
	targetSigma := rmsAmplitude * 32767.0

	out := make([]byte, len(utterancePCM))
	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xCAFE)) // deterministic — runs reproduce
	for i := 0; i+1 < len(utterancePCM); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(utterancePCM[i : i+2]))
		// Box-Muller for Gaussian noise. Two uniform → one Gaussian
		// per call; we ignore the second sample (cheaper than caching).
		u1 := rng.Float64()
		u2 := rng.Float64()
		if u1 < 1e-12 {
			u1 = 1e-12
		}
		gauss := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
		noisy := float64(sample) + gauss*targetSigma
		switch {
		case noisy > 32767:
			noisy = 32767
		case noisy < -32768:
			noisy = -32768
		}
		binary.LittleEndian.PutUint16(out[i:i+2], uint16(int16(noisy)))
	}
	return out
}
