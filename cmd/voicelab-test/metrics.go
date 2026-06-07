// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/binary"
	"math"
	"regexp"
	"strings"
)

// transcriptFidelity returns the fraction of the intended utterance
// that Grok's STT preserved, in [0, 1]. 1.0 = identical after normalising
// case, punctuation, and whitespace; 0.0 = totally mangled. Computed as
// 1 - editDistance / max(len(a), len(b)). Useful for catching cases
// where Grok hears something close-but-wrong (matters most under noise).
func transcriptFidelity(intended, heard string) float64 {
	a := normaliseTranscript(intended)
	b := normaliseTranscript(heard)
	if a == "" && b == "" {
		return 1
	}
	denom := max(len(a), len(b))
	if denom == 0 {
		return 0
	}
	d := levenshtein(a, b)
	return 1 - float64(d)/float64(denom)
}

var nonAlnumRE = regexp.MustCompile(`[^a-z0-9 ]+`)
var collapseWSRE = regexp.MustCompile(`\s+`)

func normaliseTranscript(s string) string {
	s = strings.ToLower(s)
	s = nonAlnumRE.ReplaceAllString(s, " ")
	s = collapseWSRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// levenshtein computes the classic edit distance between two strings.
// Tiny strings (under ~200 chars in the test suite); the naive 2-row
// DP is plenty fast and avoids pulling in a dep.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// audioSanity computes basic health metrics on response PCM. The
// harness uses these to catch a class of failure the LLM judge can't
// see: Grok returned a plausible transcript but the audio is silent,
// truncated, or absurdly long for the text. Two soft signals:
//
//   - durationMs: total audio length. Sub-300 ms responses to a real
//     question are almost certainly a half-formed TTS dropout.
//   - rmsDB: root-mean-square in dBFS. Above -50 dBFS means there's
//     audible signal; below means silence (or near-silence).
func audioSanity(pcm []byte) (durationMs int, rmsDB float64) {
	if len(pcm) < 2 {
		return 0, math.Inf(-1)
	}
	samples := len(pcm) / 2
	durationMs = int(float64(samples) * 1000.0 / 24000.0)

	var sumSquares float64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := float64(int16(binary.LittleEndian.Uint16(pcm[i : i+2])))
		sumSquares += s * s
	}
	rms := math.Sqrt(sumSquares / float64(samples))
	if rms <= 0 {
		return durationMs, math.Inf(-1)
	}
	rmsDB = 20 * math.Log10(rms/32767.0)
	return durationMs, rmsDB
}

