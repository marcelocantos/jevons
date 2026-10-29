// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package superviselog names the log file supervisord captures for the
// development daemon and hardens boot-line durability (🎯T771).
//
// On 2026-09-21 restarts left later lines from a live jevonsd in
// ~/.local/var/log/jevonsd.log but not the boot markers (listening,
// cockpit converge loop started), so "did the loop start?" could not be
// answered from the log alone.
package superviselog

import (
	"io"
	"os"
	"path/filepath"
)

// CapturedPath is stdout_logfile for program:jevonsd (supervisor/jevonsd.ini).
func CapturedPath(home string) string {
	if home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "var", "log", "jevonsd.log")
}

// Supervised reports whether supervisord started this process (it sets
// SUPERVISOR_ENABLED=1 in the child environment).
func Supervised() bool {
	return os.Getenv("SUPERVISOR_ENABLED") == "1"
}

// SlogWriter returns w, or a writer that fsyncs after each Write when
// supervisord captures stderr into CapturedPath.
func SlogWriter(w io.Writer) io.Writer {
	if !Supervised() {
		return w
	}
	return &syncWriter{w: w}
}

type syncWriter struct {
	w io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		return n, err
	}
	if f, ok := s.w.(*os.File); ok {
		_ = f.Sync()
	}
	return n, err
}
