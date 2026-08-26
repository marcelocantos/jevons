// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/imagetext"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

const (
	j19Prefix     = "ROOThist-"
	j19SeedTurns  = 24
	j19SlotTail   = 24
	j19MinMarkers = 8
	j19LiveToken  = "ROOThist-LIVE"
	j19LastToken  = "ROOThist-23"
)

// j19RootHistoryPaint is the 🎯T491 / 🎯T493 / 🎯T494 oracle: seed
// distinct sealed owner turns into the *isolate* journal (never the
// owner's daily history), hard-load the isolate cockpit, and assert
// the replay both keeps one virtual-list row per turn and *renders*
// those turns (checkVisibility + centre hit-test + Vision OCR).
// 🎯T540.1 journey-connect: retarget this hard-load at the React
// surface (ui build or :5173 proxy) — do not add a second connect-tail
// journey. Residual until T540.2: isolate GET / may still be vanilla.
func (s *suite) j19RootHistoryPaint() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}

	journal := filepath.Join(s.stateDir, "chatlog", overseerName+".jsonl")
	if err := seedJ19Journal(journal, j19SeedTurns); err != nil {
		return fmt.Errorf("seed journal: %w", err)
	}
	if n := countJournalMarkers(journal, j19Prefix); n < j19SeedTurns {
		return fmt.Errorf("seed wrote %d %s* user turns, want %d (%s)", n, j19Prefix, j19SeedTurns, journal)
	}

	// Paint census FIRST against the isolate seed only — never the
	// owner's daily journal. A live Grok hang must not hide a blank
	// pane. Agent interaction follows so a green paint still satisfies 🎯T107.
	shot := filepath.Join(s.stateDir, "j19-messages.png")
	paintErr := s.runJ19Paint(shot)
	ocrErr := assertJ19OCR(shot)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		if paintErr != nil {
			return paintErr
		}
		if ocrErr != nil {
			return ocrErr
		}
		if out := asOutage("j19 live dial", err); out != nil {
			return out
		}
		return fmt.Errorf("live dial: %w", err)
	}
	if _, err := drainReplay(frames, 800*time.Millisecond); err != nil {
		conn.CloseNow()
		if paintErr != nil {
			return paintErr
		}
		if ocrErr != nil {
			return ocrErr
		}
		return fmt.Errorf("pre-send replay drain: %w", err)
	}
	// T107: deliver one owner turn onto the live overseer wire. The
	// paint census is the T491 oracle; a Grok hang after accept is not
	// the collapse this journey exists to catch (J2 owns terminal).
	prompt := "Reply with exactly: " + j19LiveToken
	sendErr := conn.Write(ctx, websocket.MessageText, []byte(prompt))
	conn.CloseNow()

	if paintErr != nil {
		return paintErr
	}
	if ocrErr != nil {
		return ocrErr
	}
	if sendErr != nil {
		if out := asOutage("j19 live send", sendErr); out != nil {
			return out
		}
		return fmt.Errorf("live send write: %w", sendErr)
	}
	return nil
}

func (s *suite) runJ19Paint(screenshot string) error {
	script, err := j19PaintScript()
	if err != nil {
		return err
	}
	cmd := exec.Command("node", script,
		"--host", s.host,
		"--prefix", j19Prefix,
		"--min", fmt.Sprint(j19MinMarkers),
		"--expect", fmt.Sprint(j19SeedTurns),
		"--screenshot", screenshot,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = filepath.Clean(filepath.Join(filepath.Dir(script), "..", ".."))
	runErr := cmd.Run()
	out := stdout.String()
	if strings.TrimSpace(out) != "" {
		fmt.Println(out)
	}
	if errOut := strings.TrimSpace(stderr.String()); errOut != "" {
		fmt.Fprintln(os.Stderr, errOut)
	}
	if runErr == nil {
		return nil
	}
	// Node exit 2 is usage / daily-port (harness), not the product collapse.
	if ee, ok := runErr.(*exec.ExitError); ok && ee.ExitCode() == 2 {
		return fmt.Errorf("j19 paint harness: %s", trim(stderr.String(), 240))
	}
	if strings.Contains(out, `"emptyPane": true`) || strings.Contains(stderr.String(), "empty pane") {
		return fmt.Errorf("connect replay painted no visible turns (🎯T494): %s",
			trim(firstNonEmpty(stderr.String(), collapseSummary(out)), 400))
	}
	if strings.Contains(out, `"collapsed": true`) || strings.Contains(stderr.String(), "paint collapse") {
		return fmt.Errorf("connect replay collapsed the painted list (🎯T491): %s",
			trim(firstNonEmpty(stderr.String(), collapseSummary(out)), 400))
	}
	return fmt.Errorf("j19 paint: %w", runErr)
}

func assertJ19OCR(shot string) error {
	if _, err := os.Stat(shot); err != nil {
		return fmt.Errorf("j19 screenshot missing (%s): isolate paint did not write a viewport capture", shot)
	}
	if !imagetext.Available() {
		return &outageError{
			step:  "j19 ocr",
			class: agenterr.ClassBackendUnavailable,
			msg:   imagetext.UnavailableReason(),
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ext := imagetext.Extract(ctx, shot, "j19-messages")
	if ext.Degraded {
		return &outageError{
			step:  "j19 ocr",
			class: agenterr.ClassBackendUnavailable,
			msg:   ext.Reason,
		}
	}
	text := ext.Text()
	if j19EmptyOCRFail(j19SeedTurns, text) {
		return fmt.Errorf("j19 empty OCR + non-empty model is an empty-pane fail (🎯T493) lines=%d",
			len(ext.Lines))
	}
	hits := 0
	for i := 0; i < j19SeedTurns; i++ {
		if strings.Contains(text, fmt.Sprintf("%s%02d", j19Prefix, i)) {
			hits++
		}
	}
	if hits < 1 || !strings.Contains(text, j19LastToken) {
		return fmt.Errorf("j19 OCR missing tail token %s (hits=%d lines=%d) — pin landed past the last owner turn",
			j19LastToken, hits, len(ext.Lines))
	}
	fmt.Printf("j19 OCR: %d %s* token(s) in viewport screenshot\n", hits, j19Prefix)
	return nil
}

func seedJ19Journal(path string, turns int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	base := time.Now().UTC().Add(-time.Duration(turns+2) * time.Minute)
	var b strings.Builder
	for i := 0; i < turns; i++ {
		tok := fmt.Sprintf("%s%02d", j19Prefix, i)
		tsUser := base.Add(time.Duration(i*2) * time.Second).Format(time.RFC3339)
		tsAsst := base.Add(time.Duration(i*2+1) * time.Second).Format(time.RFC3339)
		user := map[string]any{
			"type":      "user",
			"timestamp": tsUser,
			"message": map[string]any{
				"role":    "user",
				"content": tok + " distinctive owner turn " + fmt.Sprint(i),
			},
		}
		asst := map[string]any{
			"type":      "assistant",
			"timestamp": tsAsst,
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": j19AssistantBody(tok, i)},
				},
				"stop_reason": "end_turn",
			},
		}
		ub, _ := json.Marshal(user)
		ab, _ := json.Marshal(asst)
		b.Write(ub)
		b.WriteByte('\n')
		b.Write(ab)
		b.WriteByte('\n')
		// Daily last-30 replay includes tool_use between owner turns
		// (🎯T494.1.1). A text-only seed is a failed oracle.
		tool, _ := json.Marshal(map[string]any{
			"type": "tool_use",
			"name": "Read",
			"id":   fmt.Sprintf("j19-tool-%02d", i),
		})
		b.Write(tool)
		b.WriteByte('\n')
		// Notes after every turn. Daily replay is a mix of bubbles and
		// step-slots; a single mid-list burst is not enough for the
		// scroll-up-then-down void (🎯T494.1.2).
		for n := 0; n < 8; n++ {
			tsNote := base.Add(time.Duration(i*2+1)*time.Second + time.Duration(n+1)*time.Millisecond).Format(time.RFC3339)
			tsSys := base.Add(time.Duration(i*2+1)*time.Second + time.Duration(n+1)*time.Millisecond + time.Microsecond).Format(time.RFC3339)
			note, _ := json.Marshal(map[string]any{
				"type":      "agent_note",
				"timestamp": tsNote,
				"text":      fmt.Sprintf("[Agent pad responded] mid %02d/%02d", i, n),
			})
			sys, _ := json.Marshal(map[string]any{
				"type":      "system",
				"timestamp": tsSys,
			})
			b.Write(note)
			b.WriteByte('\n')
			b.Write(sys)
			b.WriteByte('\n')
		}
	}
	// Trailing notes after the last owner turn. Must collapse to one
	// labelled ⋯ n steps marker — not 24 blank 16px rows.
	for i := 0; i < j19SlotTail; i++ {
		tsNote := base.Add(time.Duration(turns*2+i*2) * time.Second).Format(time.RFC3339)
		tsSys := base.Add(time.Duration(turns*2+i*2+1) * time.Second).Format(time.RFC3339)
		note, _ := json.Marshal(map[string]any{
			"type":      "agent_note",
			"timestamp": tsNote,
			"text":      fmt.Sprintf("[Agent pad responded] slot %02d", i),
		})
		sys, _ := json.Marshal(map[string]any{
			"type":      "system",
			"timestamp": tsSys,
		})
		b.Write(note)
		b.WriteByte('\n')
		b.Write(sys)
		b.WriteByte('\n')
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return err
	}
	return f.Sync()
}

// Tall even-numbered replies so estimate≠real is large. Short seed
// turns remat with almost no delta and J19 cannot see 🎯T494.1.2.
func j19AssistantBody(tok string, i int) string {
	// Near-end even turns are tall so connect auto-expands them (14rem
	// clip). Scroll-up collapses them, prefix shrinks, canvas min-height
	// keeps the expanded peak — the owner void (🎯T494.1.2).
	if i != 18 && i != 20 {
		return "ack " + tok
	}
	var b strings.Builder
	b.WriteString("ack ")
	b.WriteString(tok)
	b.WriteString("\n\n")
	for n := 0; n < 40; n++ {
		b.WriteString("Estimate-vs-measure padding ")
		b.WriteString(tok)
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(n))
		b.WriteByte('\n')
	}
	return b.String()
}

type j19SeedMix struct {
	User, Assistant, AgentNote, System, ToolUse int
	NotesBetweenTurns, ToolsBetweenTurns        int
}

func classifyJ19Seed(body []byte) j19SeedMix {
	var mix j19SeedMix
	phase := "start"
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var d struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(line), &d)
		switch d.Type {
		case "user":
			mix.User++
			phase = "in-turn"
		case "assistant":
			mix.Assistant++
			phase = "between"
		case "agent_note":
			mix.AgentNote++
			if phase == "between" {
				mix.NotesBetweenTurns++
			}
		case "system":
			mix.System++
		case "tool_use":
			mix.ToolUse++
			if phase == "between" {
				mix.ToolsBetweenTurns++
			}
		}
	}
	return mix
}

func countJournalMarkers(path, prefix string) int {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.Contains(line, `"type":"user"`) && !strings.Contains(line, `"type": "user"`) {
			continue
		}
		if strings.Contains(line, prefix) {
			n++
		}
	}
	return n
}

// j19EmptyOCRFail is 🎯T493: empty OCR of a non-empty model is an empty
// pane, never a green invented from the DOM. Degraded OCR is OUTAGE, not this.
func j19EmptyOCRFail(modelRows int, ocrText string) bool {
	return modelRows > 0 && strings.TrimSpace(ocrText) == ""
}

func j19PaintScript() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	script := filepath.Join(filepath.Dir(file), "j19_paint.js")
	if st, err := os.Stat(script); err != nil || st.IsDir() {
		return "", fmt.Errorf("j19_paint.js missing: %s", script)
	}
	return script, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func collapseSummary(stdout string) string {
	var report struct {
		Collapsed bool `json:"collapsed"`
		Census    struct {
			TranscriptRows int      `json:"transcriptRows"`
			UserEls        int      `json:"userEls"`
			Attached       int      `json:"attached"`
			UniqueVIndex   int      `json:"uniqueVIndex"`
			UniqueTops     int      `json:"uniqueTops"`
			StackedAt0     int      `json:"stackedAt0"`
			Markers        []string `json:"markers"`
		} `json:"census"`
	}
	if json.Unmarshal([]byte(stdout), &report) != nil {
		return trim(stdout, 240)
	}
	return fmt.Sprintf("transcriptRows=%d userEls=%d attached=%d uniqueVIndex=%d uniqueTops=%d stackedAt0=%d markers=%d",
		report.Census.TranscriptRows, report.Census.UserEls, report.Census.Attached,
		report.Census.UniqueVIndex, report.Census.UniqueTops, report.Census.StackedAt0,
		len(report.Census.Markers))
}
