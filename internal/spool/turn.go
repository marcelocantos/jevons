// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Turn is the searchable digest for one prompt a sidecar seat accepted
// (🎯T870). It is a spool line of type "turn". turn_end may still carry
// an agent snapshot for resume; a search does not decode that line.
type Turn struct {
	TS          string `json:"ts"`
	Seat        string `json:"seat"`
	Type        string `json:"type"`
	TurnID      string `json:"turn_id"`
	SessionID   string `json:"session_id"`
	Cause       string `json:"cause"`
	CauseDetail string `json:"cause_detail"`
	StartedAt   string `json:"started_at"`
	EndedAt     string `json:"ended_at"`
	Stop        string `json:"stop"`
	ToolCalls   int    `json:"tool_calls"`
	Deltas      int    `json:"deltas"`
	Chars       int    `json:"chars"`
	StopToken   string `json:"stop_token,omitempty"`
	Resume      string `json:"resume,omitempty"`
}

// TurnQuery selects digest lines. Empty fields match every turn.
type TurnQuery struct {
	Seat      string
	SessionID string
}

// QueryTurns reads digest lines for q, older dates first. Lines that
// are not type "turn" are skipped once their type is known, so a
// turn_end snapshot is not decoded.
func QueryTurns(dir string, q TurnQuery) ([]Turn, error) {
	if dir == "" {
		return nil, nil
	}
	names, err := listDayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Turn
	for _, name := range names {
		got, err := queryTurnFile(filepath.Join(dir, name), q)
		if err != nil {
			return out, err
		}
		out = append(out, got...)
	}
	return out, nil
}

func queryTurnFile(path string, q TurnQuery) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	var out []Turn
	for {
		line, err := nextDigestLine(r)
		if len(line) > 0 {
			var t Turn
			if json.Unmarshal(line, &t) == nil && t.Type == "turn" && turnMatches(t, q) {
				out = append(out, t)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

func turnMatches(t Turn, q TurnQuery) bool {
	if q.Seat != "" && t.Seat != q.Seat {
		return false
	}
	if q.SessionID != "" && t.SessionID != q.SessionID {
		return false
	}
	return true
}

// LineType reads the first "type" string in a spool line. It stops at
// that field, which the sidecar writes ahead of any snapshot.
func LineType(line []byte) string {
	key := []byte(`"type"`)
	i := bytes.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	rest = bytes.TrimLeft(rest, " \t")
	if len(rest) == 0 || rest[0] != ':' {
		return ""
	}
	rest = bytes.TrimLeft(rest[1:], " \t")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return string(rest[:j])
}

// nextDigestLine returns the next type=turn line. Other lines are
// discarded after the type field is known, without retaining a snapshot.
func nextDigestLine(r *bufio.Reader) ([]byte, error) {
	var held []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			held = append(held, chunk...)
			if typ := typeFromPrefix(held); typ != "" && typ != "turn" {
				held = nil
				for err == bufio.ErrBufferFull {
					_, err = r.ReadSlice('\n')
				}
				if err == io.EOF {
					return nil, io.EOF
				}
				if err != nil {
					return nil, err
				}
				continue
			}
			continue
		}
		if len(chunk) > 0 {
			held = append(held, chunk...)
		}
		line := bytes.TrimSpace(held)
		held = nil
		if len(line) == 0 {
			if err == io.EOF {
				return nil, io.EOF
			}
			if err != nil {
				return nil, err
			}
			continue
		}
		if LineType(line) != "turn" {
			if err == io.EOF {
				return nil, io.EOF
			}
			if err != nil {
				return nil, err
			}
			continue
		}
		return line, err
	}
}

func typeFromPrefix(held []byte) string {
	n := len(held)
	if n > 512 {
		n = 512
	}
	return LineType(held[:n])
}

// VisibleAssistantText drops a stop token from owner-visible text.
func VisibleAssistantText(s string) string {
	visible, _ := stripStopToken(s)
	return visible
}

func stripStopToken(s string) (visible, token string) {
	tokens := []string{"<|endoftext|>", "<|im_end|>", "<|eot_id|>", "<|eos|>", "<|eot|>"}
	visible = s
	for _, tok := range tokens {
		if strings.Contains(visible, tok) {
			if token == "" {
				token = tok
			}
			visible = strings.ReplaceAll(visible, tok, "")
		}
	}
	return visible, token
}
