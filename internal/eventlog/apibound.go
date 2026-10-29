// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"strings"
)

// MaxLogsAPIResponseBytes is the maximum encoded GET /api/logs body (🎯T1172).
// Health probes (e.g. sen) read with limit=20 and must stay under this cap.
const MaxLogsAPIResponseBytes = 64 << 10

// MaxLogsAPIEntryMsgBytes caps each event's msg field in GET /api/logs JSON.
const MaxLogsAPIEntryMsgBytes = 1024

// MaxLogsAPIFieldStringBytes caps each string value under fields in GET /api/logs JSON.
const MaxLogsAPIFieldStringBytes = 512

const apiTruncationMarker = "…[truncated]"

// LogsPageResponse is the JSON shape for GET /api/logs.
type LogsPageResponse struct {
	Path              string        `json:"path"`
	Count             int           `json:"count"`
	Events            []APILogEvent `json:"events"`
	NextCursor        *int64        `json:"next_cursor,omitempty"`
	HeadCursor        int64         `json:"head_cursor"`
	ScannedBytes      int64         `json:"scanned_bytes"`
	ResponseTruncated bool          `json:"response_truncated,omitempty"`
}

// APILogEvent is one journal row as returned by GET /api/logs. Large msg and
// field payloads are shortened; Truncated is set when anything was cut.
type APILogEvent struct {
	TS        string         `json:"ts"`
	Source    string         `json:"source"`
	Level     string         `json:"level"`
	Msg       string         `json:"msg"`
	Component string         `json:"component,omitempty"`
	Decision  string         `json:"decision,omitempty"`
	Corr      string         `json:"corr,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
}

// MarshalLogsPage encodes a page for GET /api/logs with per-entry and total
// body caps (🎯T1172).
func MarshalLogsPage(path string, page PageResult) ([]byte, error) {
	resp := buildLogsPageResponse(path, page, MaxLogsAPIEntryMsgBytes, MaxLogsAPIFieldStringBytes)
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	if len(body) <= MaxLogsAPIResponseBytes {
		return body, nil
	}
	msgCap, fieldCap := MaxLogsAPIEntryMsgBytes, MaxLogsAPIFieldStringBytes
	for msgCap > 64 || fieldCap > 32 {
		msgCap /= 2
		if fieldCap > 32 {
			fieldCap /= 2
		}
		resp = buildLogsPageResponse(path, page, msgCap, fieldCap)
		body, err = json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if len(body) <= MaxLogsAPIResponseBytes {
			return body, nil
		}
	}
	return body, nil
}

func buildLogsPageResponse(path string, page PageResult, msgCap, fieldCap int) LogsPageResponse {
	events := make([]APILogEvent, 0, len(page.Events))
	responseTrunc := false
	for _, ev := range page.Events {
		apiEv, trunc := boundAPILogEvent(ev, msgCap, fieldCap)
		if trunc {
			responseTrunc = true
		}
		events = append(events, apiEv)
	}
	return LogsPageResponse{
		Path:              path,
		Count:             len(events),
		Events:            events,
		NextCursor:        page.NextCursor,
		HeadCursor:        page.HeadCursor,
		ScannedBytes:      page.ScannedBytes,
		ResponseTruncated: responseTrunc,
	}
}

func boundAPILogEvent(ev Event, msgCap, fieldCap int) (APILogEvent, bool) {
	trunc := false
	msg := ev.Msg
	if len(msg) > msgCap {
		msg = truncateUTF8(msg, msgCap)
		trunc = true
	}
	fields := ev.Fields
	if len(fields) > 0 {
		next := make(map[string]any, len(fields))
		for k, v := range fields {
			bv, cut := boundFieldValue(v, fieldCap)
			if cut {
				trunc = true
			}
			next[k] = bv
		}
		fields = next
	}
	return APILogEvent{
		TS:        ev.TS,
		Source:    ev.Source,
		Level:     ev.Level,
		Msg:       msg,
		Component: ev.Component,
		Decision:  ev.Decision,
		Corr:      ev.Corr,
		Fields:    fields,
		Truncated: trunc,
	}, trunc
}

func boundFieldValue(v any, cap int) (any, bool) {
	switch val := v.(type) {
	case string:
		if len(val) <= cap {
			return val, false
		}
		return truncateUTF8(val, cap), true
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return v, false
		}
		if len(b) <= cap {
			return v, false
		}
		return truncateUTF8(string(b), cap), true
	}
}

func truncateUTF8(s string, max int) string {
	if max <= 0 {
		return ""
	}
	marker := apiTruncationMarker
	if max <= len(marker) {
		return s[:max]
	}
	limit := max - len(marker)
	if len(s) <= limit {
		return s
	}
	for cut := limit; cut > 0 && cut < len(s); cut-- {
		if s[cut]&0xC0 != 0x80 {
			return s[:cut] + marker
		}
	}
	return marker
}

// BoundAPILogEvent exports single-event bounding for tests and callers.
func BoundAPILogEvent(ev Event) (APILogEvent, bool) {
	return boundAPILogEvent(ev, MaxLogsAPIEntryMsgBytes, MaxLogsAPIFieldStringBytes)
}

// HasAPITruncationMarker reports whether s ends with the documented suffix.
func HasAPITruncationMarker(s string) bool {
	return strings.HasSuffix(s, apiTruncationMarker)
}
