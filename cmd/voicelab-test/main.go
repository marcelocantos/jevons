// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// voicelab-test runs an automated suite of voice round-trips against
// Grok Realtime: each case TTSes a known utterance (optionally with
// noise overlaid), plays it into the voicelab.Loop at realtime
// cadence, records what Grok heard and what it said back, then judges
// the result. Pass/fail per case + a metric table at the end. Useful
// for catching regressions in the voice path without having to drive
// every change manually.
//
// Two grading modes per case:
//   - ExpectAny: case-insensitive substring match on the response
//     transcript (cheap, deterministic).
//   - JudgeRubric: claude -p reads the rubric + transcripts and
//     returns {"ok": bool, "notes": str} (for open-ended answers).
//
// Modes:
//   - default text output
//   - --json: machine-readable result array on stdout
//   - --baseline-write=PATH: after a run, dump latencies to PATH
//   - --baseline-read=PATH: fail any case whose latency exceeds
//     the baseline's by more than --baseline-tolerance (default 50%).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/voicelab"
)

func main() {
	verbose := flag.Bool("v", false, "verbose protocol logging")
	timeout := flag.Duration("timeout", 15*time.Second, "per-case hard timeout")
	claudeBin := flag.String("claude", expandHome("~/.local/bin/claude"), "path to the claude CLI for the judge")
	scratch := flag.String("scratch", "/tmp/voicelab-test", "scratch dir for TTS WAV files")
	filterName := flag.String("only", "", "if set, run only the case with this name")
	jsonOut := flag.Bool("json", false, "emit results as JSON on stdout (suppresses pretty text)")
	noColor := flag.Bool("no-color", false, "disable ANSI colour in pretty text output")
	baselineRead := flag.String("baseline-read", "", "compare latencies against this baseline JSON; fail on regression")
	baselineWrite := flag.String("baseline-write", "", "after a run, write per-case latencies to this path")
	baselineTol := flag.Float64("baseline-tolerance", 0.5, "regression threshold: fraction by which latency may exceed baseline (0.5 = 50%)")
	dumpWAVs := flag.String("dump-wavs", "", "if set, write each case's response audio to <dir>/<case>.wav for manual audit")
	flag.Parse()

	logLevel := slog.LevelError
	if *verbose {
		logLevel = slog.LevelDebug
	}
	base := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	if *verbose {
		// In verbose mode, surface everything — including the shutdown
		// race log — so a real protocol problem isn't accidentally hidden.
		slog.SetDefault(slog.New(base))
	} else {
		slog.SetDefault(slog.New(quietHandler{inner: base}))
	}

	apiKey, err := loadKeychainKey("xai-api-key")
	if err != nil {
		fatal("xai-api-key not found in keychain: %v", err)
	}

	if _, err := exec.LookPath(*claudeBin); err != nil {
		fmt.Fprintf(os.Stderr, "warning: claude not found at %s — rubric cases will be skipped\n", *claudeBin)
	}

	baseline, err := readBaseline(*baselineRead)
	if err != nil {
		fatal("read baseline %s: %v", *baselineRead, err)
	}

	useColor := !*noColor && !*jsonOut && isTerminal(os.Stderr)

	results := make([]result, 0, len(Cases))
	for _, c := range Cases {
		if *filterName != "" && c.Name != *filterName {
			continue
		}
		if !*jsonOut {
			fmt.Fprintf(os.Stderr, "=== %s\n", c.Name)
		}
		r := runCase(c, apiKey, *scratch, *timeout, *claudeBin)
		if base, ok := baseline[c.Name]; ok && r.LatencyMeasured {
			budget := time.Duration(float64(base.LatencyMs) * (1 + *baselineTol) * float64(time.Millisecond))
			r.BaselineLatency = time.Duration(base.LatencyMs) * time.Millisecond
			if r.Latency > budget {
				r.Regression = true
			}
		}
		if *dumpWAVs != "" && len(r.ResponseAudio) > 0 {
			wavPath := filepath.Join(*dumpWAVs, c.Name+".wav")
			if err := dumpWAV(wavPath, r.ResponseAudio); err != nil {
				fmt.Fprintf(os.Stderr, "warning: dump-wav %s: %v\n", wavPath, err)
			}
		}
		results = append(results, r)
		if !*jsonOut {
			printShortResult(r, useColor)
		}
	}

	if *baselineWrite != "" {
		if err := writeBaseline(*baselineWrite, results); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write baseline %s: %v\n", *baselineWrite, err)
		}
	}

	if *jsonOut {
		if err := emitJSON(os.Stdout, results); err != nil {
			fatal("emit json: %v", err)
		}
	} else {
		printSummary(results, useColor)
	}

	for _, r := range results {
		if !r.passed() {
			os.Exit(1)
		}
	}
}

type result struct {
	Case            Case
	UserTranscript  string
	ResponseText    string
	ResponseAudio   []byte // PCM16 24 kHz mono, what Grok TTSed
	Latency         time.Duration
	LatencyMeasured bool
	Fidelity        float64 // 0–1, intended utterance vs heard transcript
	AudioMs         int     // response audio length
	AudioRMSDB      float64 // dBFS; <-50 → effectively silent
	JudgeOK         bool
	JudgeNotes      string
	JudgeSkipped    bool
	SubstringHit    string
	BaselineLatency time.Duration
	Regression      bool
	Err             error
}

func (r result) passed() bool {
	if r.Err != nil {
		return false
	}
	if r.LatencyMeasured && r.Case.MaxLatency > 0 && r.Latency > r.Case.MaxLatency {
		return false
	}
	if r.Regression {
		return false
	}
	// Audio sanity: a "successful" round-trip whose response audio is
	// silent or truncated isn't really success. Thresholds are loose
	// (caught at 300 ms / -50 dBFS) — they exist to flag TTS drop-outs
	// the LLM judge can't see, not to nitpick voice quality.
	if r.AudioMs > 0 && r.AudioMs < 300 {
		return false
	}
	if !math.IsInf(r.AudioRMSDB, -1) && r.AudioRMSDB < -50 && r.AudioMs > 0 {
		return false
	}
	if len(r.Case.ExpectAny) > 0 {
		return r.SubstringHit != ""
	}
	if r.Case.JudgeRubric != "" && !r.JudgeSkipped {
		return r.JudgeOK
	}
	return true
}

func runCase(c Case, apiKey, scratch string, timeout time.Duration, claudeBin string) result {
	r := result{Case: c}

	caseScratch := filepath.Join(scratch, c.Name)
	utterancePCM, err := synth(c.Utterance, caseScratch)
	if err != nil {
		r.Err = fmt.Errorf("synth: %w", err)
		return r
	}
	if c.NoiseRMS > 0 {
		utterancePCM = mixNoise(utterancePCM, c.NoiseRMS)
	}
	silence := silencePCM(400) // small trailing silence; not VAD-critical in ManualCommit
	combined := make([]byte, 0, len(utterancePCM)+len(silence))
	combined = append(combined, utterancePCM...)
	combined = append(combined, silence...)

	var (
		stampMu      sync.Mutex
		utteranceEnd time.Time
		userTextMu   sync.Mutex
		userText     strings.Builder
		responseMu   sync.Mutex
		response     strings.Builder
		responseDone = make(chan struct{})
		responseOnce sync.Once
	)

	source := &voicelab.BufferSource{
		PCM:        combined,
		StampAfter: len(utterancePCM),
		OnStamp: func() {
			stampMu.Lock()
			utteranceEnd = time.Now()
			stampMu.Unlock()
		},
	}
	sink := &voicelab.BufferSink{}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	loop := &voicelab.Loop{
		APIKey:       apiKey,
		Voice:        "Eve",
		SystemPrompt: "You are a voice assistant being smoke-tested. Reply briefly and directly.",
		ManualCommit: true,
		Source:       source,
		Sink:         sink,
		OnUserTranscript: func(text string) {
			userTextMu.Lock()
			userText.WriteString(text)
			userTextMu.Unlock()
		},
		OnTranscript: func(text string) {
			responseMu.Lock()
			response.WriteString(text)
			responseMu.Unlock()
		},
		OnResponseDone: func() {
			responseOnce.Do(func() { close(responseDone) })
		},
		OnError: func(err error) {
			slog.Debug("loop err", "err", err)
		},
	}

	loopErr := make(chan error, 1)
	go func() { loopErr <- loop.Run(ctx) }()

	select {
	case <-responseDone:
	case <-ctx.Done():
	}
	cancel()
	<-loopErr

	r.UserTranscript = strings.TrimSpace(userText.String())
	r.ResponseText = strings.TrimSpace(response.String())
	r.ResponseAudio = sink.PCM()
	r.Fidelity = transcriptFidelity(c.Utterance, r.UserTranscript)
	r.AudioMs, r.AudioRMSDB = audioSanity(r.ResponseAudio)

	stampMu.Lock()
	ue := utteranceEnd
	stampMu.Unlock()
	first := sink.FirstFrameAt()
	if !ue.IsZero() && !first.IsZero() && !first.Before(ue) {
		r.Latency = first.Sub(ue)
		r.LatencyMeasured = true
	}

	if ctx.Err() == context.DeadlineExceeded && r.ResponseText == "" {
		r.Err = fmt.Errorf("timed out before response.done")
		return r
	}

	if len(c.ExpectAny) > 0 {
		respLower := strings.ToLower(r.ResponseText)
		for _, want := range c.ExpectAny {
			if strings.Contains(respLower, strings.ToLower(want)) {
				r.SubstringHit = want
				break
			}
		}
	} else if c.JudgeRubric != "" {
		if _, err := exec.LookPath(claudeBin); err != nil {
			r.JudgeSkipped = true
			r.JudgeNotes = "claude binary not available"
		} else {
			v, raw, err := judge(claudeBin, c.JudgeRubric, c.Utterance, r.UserTranscript, r.ResponseText)
			if err != nil {
				r.JudgeSkipped = true
				r.JudgeNotes = "judge error: " + err.Error()
			} else {
				r.JudgeOK = v.OK
				if v.Notes != "" {
					r.JudgeNotes = v.Notes
				} else if !v.OK {
					r.JudgeNotes = "(no notes; raw: " + raw + ")"
				}
			}
		}
	}

	return r
}

// printShortResult writes the per-case summary in human-readable form.
// useColor enables ANSI codes for PASS/FAIL so failures pop in a busy
// terminal; disable for piped/non-terminal output.
func printShortResult(r result, useColor bool) {
	tag := tagFor(r, useColor)
	if r.Err != nil {
		fmt.Fprintf(os.Stderr, "  %s — %s\n\n", tag, r.Err)
		return
	}
	fmt.Fprintf(os.Stderr, "  heard: %q\n", r.UserTranscript)
	fmt.Fprintf(os.Stderr, "  said:  %q\n", r.ResponseText)
	if r.LatencyMeasured {
		extra := ""
		if r.BaselineLatency > 0 {
			extra = fmt.Sprintf(" (baseline %s)", r.BaselineLatency.Round(time.Millisecond))
		}
		fmt.Fprintf(os.Stderr, "  latency: %s (budget %s)%s\n", r.Latency.Round(time.Millisecond), r.Case.MaxLatency, extra)
	}
	fmt.Fprintf(os.Stderr, "  fidelity: %.0f%%   audio: %dms @ %.1fdBFS\n",
		r.Fidelity*100, r.AudioMs, r.AudioRMSDB)
	if r.Regression {
		fmt.Fprintf(os.Stderr, "  %s\n", colorize("REGRESSION vs baseline", "31", useColor))
	}
	switch {
	case len(r.Case.ExpectAny) > 0:
		if r.SubstringHit != "" {
			fmt.Fprintf(os.Stderr, "  match: %q\n", r.SubstringHit)
		} else {
			fmt.Fprintf(os.Stderr, "  match: none of %v\n", r.Case.ExpectAny)
		}
	case r.Case.JudgeRubric != "":
		switch {
		case r.JudgeSkipped:
			fmt.Fprintf(os.Stderr, "  judge: SKIPPED — %s\n", r.JudgeNotes)
		case r.JudgeOK:
			fmt.Fprintf(os.Stderr, "  judge: OK — %s\n", r.JudgeNotes)
		default:
			fmt.Fprintf(os.Stderr, "  judge: NOT OK — %s\n", r.JudgeNotes)
		}
	}
	fmt.Fprintf(os.Stderr, "  %s\n\n", tag)
}

func tagFor(r result, useColor bool) string {
	if r.passed() {
		return colorize("PASS", "32", useColor)
	}
	return colorize("FAIL", "31", useColor)
}

func colorize(s, code string, on bool) string {
	if !on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func printSummary(rs []result, useColor bool) {
	pass, fail := 0, 0
	for _, r := range rs {
		if r.passed() {
			pass++
		} else {
			fail++
		}
	}
	passStr := fmt.Sprintf("%d passed", pass)
	failStr := fmt.Sprintf("%d failed", fail)
	if useColor && pass > 0 {
		passStr = colorize(passStr, "32", true)
	}
	if useColor && fail > 0 {
		failStr = colorize(failStr, "31", true)
	}
	fmt.Fprintf(os.Stderr, "summary: %s, %s\n", passStr, failStr)
}

// --- JSON / baseline persistence ---

type jsonCase struct {
	Name           string  `json:"name"`
	Utterance      string  `json:"utterance"`
	UserTranscript string  `json:"user_transcript"`
	ResponseText   string  `json:"response_text"`
	LatencyMs      int64   `json:"latency_ms,omitempty"`
	BudgetMs       int64   `json:"budget_ms,omitempty"`
	BaselineMs     int64   `json:"baseline_ms,omitempty"`
	Regression     bool    `json:"regression,omitempty"`
	Fidelity       float64 `json:"fidelity"`
	AudioMs        int     `json:"audio_ms"`
	AudioRMSDB     float64 `json:"audio_rms_db"`
	Mode           string  `json:"mode"` // "substring" | "judge"
	SubstringHit   string  `json:"substring_hit,omitempty"`
	JudgeOK        *bool   `json:"judge_ok,omitempty"`
	JudgeNotes     string  `json:"judge_notes,omitempty"`
	NoiseRMS       float64 `json:"noise_rms,omitempty"`
	Error          string  `json:"error,omitempty"`
	Passed         bool    `json:"passed"`
}

func emitJSON(w *os.File, rs []result) error {
	out := make([]jsonCase, len(rs))
	for i, r := range rs {
		j := jsonCase{
			Name:           r.Case.Name,
			Utterance:      r.Case.Utterance,
			UserTranscript: r.UserTranscript,
			ResponseText:   r.ResponseText,
			BudgetMs:       r.Case.MaxLatency.Milliseconds(),
			BaselineMs:     r.BaselineLatency.Milliseconds(),
			Regression:     r.Regression,
			Fidelity:       r.Fidelity,
			AudioMs:        r.AudioMs,
			AudioRMSDB:     r.AudioRMSDB,
			NoiseRMS:       r.Case.NoiseRMS,
			SubstringHit:   r.SubstringHit,
			JudgeNotes:     r.JudgeNotes,
			Passed:         r.passed(),
		}
		if r.LatencyMeasured {
			j.LatencyMs = r.Latency.Milliseconds()
		}
		if len(r.Case.ExpectAny) > 0 {
			j.Mode = "substring"
		} else if r.Case.JudgeRubric != "" {
			j.Mode = "judge"
			if !r.JudgeSkipped {
				ok := r.JudgeOK
				j.JudgeOK = &ok
			}
		}
		if r.Err != nil {
			j.Error = r.Err.Error()
		}
		out[i] = j
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

type baselineEntry struct {
	LatencyMs int64 `json:"latency_ms"`
}

func readBaseline(path string) (map[string]baselineEntry, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var b map[string]baselineEntry
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return b, nil
}

func writeBaseline(path string, rs []result) error {
	b := make(map[string]baselineEntry, len(rs))
	for _, r := range rs {
		if r.passed() && r.LatencyMeasured {
			b[r.Case.Name] = baselineEntry{LatencyMs: r.Latency.Milliseconds()}
		}
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// --- helpers ---

func loadKeychainKey(service string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", "jevons", "-s", service, "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(os.Getenv("HOME"), p[2:])
	}
	return p
}

// isTerminal returns true when f looks like an interactive TTY. We
// avoid pulling in golang.org/x/term just for this; the cheap check
// (stat.Mode has CharDevice) is enough — a pipe / file / null device
// all read as non-terminal.
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "voicelab-test: "+format+"\n", args...)
	os.Exit(1)
}
