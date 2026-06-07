// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "time"

// Case is one test scenario: a synthetic utterance fed into the voice
// loop with expectations on the response.
//
// Exactly one of ExpectAny or JudgeRubric should be set. ExpectAny is
// a cheap deterministic check ("did the response contain '4' or
// 'four'?") for cases with a known answer; JudgeRubric defers to
// claude -p for cases where the answer space is open ("did this
// reply make sense as a response to '<utterance>'?").
type Case struct {
	Name       string
	Utterance  string
	MaxLatency time.Duration

	// NoiseRMS overlays Gaussian white noise on the utterance audio
	// at the given RMS amplitude (0.0–1.0). Values 0.05–0.2 are a
	// useful stress range for car-cabin-style interference.
	NoiseRMS float64

	// Deterministic check: pass if any of these substrings appears
	// (case-insensitive) anywhere in the response transcript.
	ExpectAny []string

	// LLM-judged check: prompt fragment appended to the judge rubric.
	// Phrased as a yes/no question about the response.
	JudgeRubric string
}

// Cases is the seed suite. Keep utterances short and unambiguous —
// the goal is to exercise the loop, not stress Grok's reasoning.
var Cases = []Case{
	{
		Name:       "arithmetic",
		Utterance:  "What is two plus two?",
		MaxLatency: 3 * time.Second,
		ExpectAny:  []string{"4", "four"},
	},
	{
		Name:       "greeting",
		Utterance:  "Hello, how are you today?",
		MaxLatency: 3 * time.Second,
		JudgeRubric: "Pass if the reply acknowledges the greeting in a natural conversational way (any tone, any length). Fail if it ignores the greeting or responds with a non-sequitur.",
	},
	{
		Name:       "short_fact",
		Utterance:  "Tell me one short fact about Mars.",
		MaxLatency: 4 * time.Second,
		JudgeRubric: "Pass if the reply states a factual claim about Mars (the planet). Fail if it refuses, asks a clarifying question, or talks about something other than Mars.",
	},
	{
		Name:       "follow_up_intent",
		Utterance:  "Can you set a timer for five minutes?",
		MaxLatency: 4 * time.Second,
		JudgeRubric: "Pass if the reply either (a) confirms setting the timer, or (b) clearly explains that it cannot set timers and offers something useful. Fail if the response is confused or off-topic.",
	},
	{
		Name:       "longer_response",
		Utterance:  "Give me three quick safety tips for driving in heavy rain.",
		MaxLatency: 5 * time.Second,
		JudgeRubric: "Pass if the reply lists multiple distinct heavy-rain driving tips (3 is ideal but 2-4 is acceptable). Fail if the response is vague, off-topic, or only gives one tip.",
	},
	{
		Name:       "yes_no_question",
		Utterance:  "Is the sky blue on Earth?",
		MaxLatency: 3 * time.Second,
		ExpectAny:  []string{"yes", "blue"},
	},
	{
		Name:       "arithmetic_with_noise",
		Utterance:  "What is three times five?",
		MaxLatency: 7 * time.Second,
		NoiseRMS:   0.08, // moderate stress — still clearly audible speech
		ExpectAny:  []string{"15", "fifteen"},
	},
	{
		Name:       "polite_refusal_intent",
		Utterance:  "Please delete all my emails right now.",
		MaxLatency: 4 * time.Second,
		JudgeRubric: "Pass if the reply either (a) explains that it cannot perform that action, or (b) asks for confirmation before doing something destructive. Fail if the response pretends to have done it or is dismissive/rude.",
	},
}
