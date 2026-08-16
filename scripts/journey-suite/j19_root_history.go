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

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

const (
	j19Prefix     = "ROOThist-"
	j19SeedTurns  = 16
	j19MinMarkers = 8
	j19LiveToken  = "ROOThist-LIVE"
)

// j19RootHistoryPaint is the 🎯T491 oracle: seed many distinct sealed
// owner turns, drive one live overseer send, then open the isolate
// cockpit and assert the virtual-list model did not collapse to one
// leftover bubble stacked at top:0. Product is currently red — this
// journey exists to demonstrate that error before the fix.
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

	// Paint census FIRST. The daily collapse is a hard-connect replay
	// bug; a live Grok hang must not hide it. Agent interaction follows
	// so a green paint still satisfies 🎯T107.
	paintErr := s.runJ19Paint()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		if paintErr != nil {
			return paintErr
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
	if sendErr != nil {
		if out := asOutage("j19 live send", sendErr); out != nil {
			return out
		}
		return fmt.Errorf("live send write: %w", sendErr)
	}
	return nil
}

func (s *suite) runJ19Paint() error {
	script, err := j19PaintScript()
	if err != nil {
		return err
	}
	cmd := exec.Command("node", script,
		"--host", s.host,
		"--prefix", j19Prefix,
		"--min", fmt.Sprint(j19MinMarkers),
		"--expect", fmt.Sprint(j19SeedTurns),
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
	if strings.Contains(out, `"collapsed": true`) || strings.Contains(stderr.String(), "paint collapse") {
		return fmt.Errorf("connect replay collapsed the painted list (🎯T491): %s",
			trim(firstNonEmpty(stderr.String(), collapseSummary(out)), 400))
	}
	return fmt.Errorf("j19 paint: %w", runErr)
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
					map[string]any{"type": "text", "text": "ack " + tok},
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
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return err
	}
	return f.Sync()
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
