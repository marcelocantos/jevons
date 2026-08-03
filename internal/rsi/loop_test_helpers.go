// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// fileJournal wraps eventlog.Journal for tests that need raw field maps.
type fileJournal struct {
	j *eventlog.Journal
}

func newFileJournal(path string) (*fileJournal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	j, err := eventlog.Open(path)
	if err != nil {
		return nil, err
	}
	return &fileJournal{j: j}, nil
}

func (f *fileJournal) Append(m map[string]any) error {
	ev := eventlog.Event{
		TS:        strField(m, "ts"),
		Source:    strField(m, "source"),
		Level:     strField(m, "level"),
		Msg:       strField(m, "msg"),
		Component: strField(m, "component"),
		Decision:  strField(m, "decision"),
		Corr:      strField(m, "corr"),
	}
	if fields, ok := m["fields"].(map[string]any); ok {
		ev.Fields = fields
	}
	return f.j.Append(ev)
}

func (f *fileJournal) Close() error {
	return f.j.Close()
}

func strField(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}