// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
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

// startReactSurface is the shared React load path for every UI journey.
// 🎯T540.2: isolate GET / is React when ui/dist exists; otherwise
// the helper starts the Vite proxy (never :13705). Dual-path residual.
func (s *suite) startReactSurface() (*j19ReactSurface, error) {
	return s.startJ19ReactSurface()
}

func (s *suite) runReactPaint(uiHost, scenario, screenshot string, agent ...string) error {
	if err := refuseDailyHost(uiHost); err != nil {
		return err
	}
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	script, err := reactPaintScript()
	if err != nil {
		return err
	}
	args := []string{script, "--host", uiHost, "--scenario", scenario}
	if screenshot != "" {
		args = append(args, "--screenshot", screenshot)
	}
	if len(agent) > 0 && agent[0] != "" {
		args = append(args, "--agent", agent[0])
	}
	cmd := exec.Command("node", args...)
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
	if ee, ok := runErr.(*exec.ExitError); ok && ee.ExitCode() == 2 {
		return fmt.Errorf("react paint harness: %s", trim(stderr.String(), 240))
	}
	return fmt.Errorf("react paint scenario=%s: %w\n%s", scenario, runErr, trim(firstNonEmpty(stderr.String(), out), 600))
}

// liveOwnerTouch writes one owner turn on the isolate chat wire so a
// paint-only journey still satisfies 🎯T107. J2 owns terminal.
func (s *suite) liveOwnerTouch(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		if out := asOutage("react journey live dial", err); out != nil {
			return out
		}
		return fmt.Errorf("live dial: %w", err)
	}
	if _, err := drainReplay(frames, 800*time.Millisecond); err != nil {
		conn.CloseNow()
		return fmt.Errorf("pre-send replay drain: %w", err)
	}
	prompt := "Reply with exactly: " + token
	sendErr := conn.Write(ctx, websocket.MessageText, []byte(prompt))
	conn.CloseNow()
	if sendErr != nil {
		if out := asOutage("react journey live send", sendErr); out != nil {
			return out
		}
		return fmt.Errorf("live send write: %w", sendErr)
	}
	return nil
}

func reactPaintScript() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	script := filepath.Join(filepath.Dir(file), "react_paint.js")
	if st, err := os.Stat(script); err != nil || st.IsDir() {
		return "", fmt.Errorf("react_paint.js missing: %s", script)
	}
	return script, nil
}
