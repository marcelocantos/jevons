// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// AsJSONL renders seat records as Claude-shaped JSONL so existing
// turnev / transcript / cost readers consume the dated spool (🎯T866.4).
func AsJSONL(recs []Record) []byte {
	var buf bytes.Buffer
	for _, rec := range recs {
		line := claudeLine(rec)
		if len(line) == 0 {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func claudeLine(rec Record) []byte {
	ts := rec.RawTS
	if ts == "" && !rec.TS.IsZero() {
		ts = rec.TS.Format(time.RFC3339Nano)
	}
	switch rec.Type {
	case "text", "turn_end":
		role := "assistant"
		if rec.Type == "turn_end" && rec.Text == "" {
			role = "assistant"
		}
		msg := map[string]any{
			"type":      role,
			"timestamp": ts,
			"sessionId": rec.Seat,
			"message": map[string]any{
				"role":    role,
				"model":   rec.Model,
				"content": []map[string]any{{"type": "text", "text": rec.Text}},
			},
		}
		if rec.Type == "turn_end" {
			msg["message"].(map[string]any)["stop_reason"] = "end_turn"
		}
		b, err := json.Marshal(msg)
		if err != nil {
			return nil
		}
		return b
	case "tool_call":
		b, err := json.Marshal(map[string]any{
			"type":      "assistant",
			"timestamp": ts,
			"sessionId": rec.Seat,
			"message": map[string]any{
				"role":  "assistant",
				"model": rec.Model,
				"content": []map[string]any{{
					"type":  "tool_use",
					"id":    rec.CallID,
					"name":  rec.Name,
					"input": json.RawMessage(orObject(rec.Text)),
				}},
			},
		})
		if err != nil {
			return nil
		}
		return b
	default:
		return nil
	}
}

// EnsureView writes a derived Claude-shaped JSONL for seat under dir/.view
// so path-based readers (transcript, cost tail) read the spool, not a
// vendor JSONL (🎯T866.4).
func EnsureView(dir, seat string) (string, error) {
	recs, err := ReadSeat(dir, seat)
	if err != nil || len(recs) == 0 {
		return "", err
	}
	viewDir := filepath.Join(dir, ".view")
	if err := os.MkdirAll(viewDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(viewDir, seat+".jsonl")
	if err := os.WriteFile(path, AsJSONL(recs), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func orObject(s string) []byte {
	if s == "" {
		return []byte(`{}`)
	}
	if json.Valid([]byte(s)) {
		return []byte(s)
	}
	b, _ := json.Marshal(s)
	return b
}
