// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The last 28 paired turns give the virtualizer >12,000px of real painted
// history even when older bubbles are clipped to 14rem. Keep timestamps older
// than the live turn so the new owner send remains the transcript tail.
const j32SeedTurns = 28

func seedJ32Journal(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	base := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < j32SeedTurns; i++ {
		var body strings.Builder
		for n := 0; n < 32; n++ {
			fmt.Fprintf(&body, "J32 history %02d line %02d: an independently painted line of transcript ink for the tall pin oracle.\\n", i, n)
		}
		for step, row := range []map[string]any{
			{"type": "user", "message": map[string]any{"role": "user", "content": body.String()}},
			{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": body.String()}}, "stop_reason": "end_turn"}},
		} {
			row["timestamp"] = base.Add(time.Duration(i*2+step) * time.Second).Format(time.RFC3339Nano)
			b, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				return err
			}
		}
	}
	return f.Sync()
}
