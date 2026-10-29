// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestT1172LogsAPIBoundedBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	const n = 20
	const huge = 100 << 10
	for i := 0; i < n; i++ {
		ev := Event{
			TS:        "2026-10-22T12:00:00Z",
			Source:    "server",
			Level:     "info",
			Component: "probe",
			Decision:  "flood",
			Msg:       strings.Repeat("M", huge),
			Fields: map[string]any{
				"blob": strings.Repeat("F", huge),
			},
		}
		if err := j.Append(ev); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	page, err := Page(path, nil, n, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != n {
		t.Fatalf("page count=%d want %d", len(page.Events), n)
	}

	body, err := MarshalLogsPage(path, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > MaxLogsAPIResponseBytes {
		t.Fatalf("body %d bytes exceeds cap %d", len(body), MaxLogsAPIResponseBytes)
	}

	var resp LogsPageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ResponseTruncated {
		t.Fatal("response_truncated missing")
	}
	if resp.Count != n {
		t.Fatalf("count=%d want %d", resp.Count, n)
	}
	for i, ev := range resp.Events {
		if !ev.Truncated {
			t.Fatalf("event %d: truncated flag missing", i)
		}
		if !HasAPITruncationMarker(ev.Msg) {
			t.Fatalf("event %d: msg missing truncation marker", i)
		}
		if len(ev.Msg) > MaxLogsAPIEntryMsgBytes+len(apiTruncationMarker) {
			t.Fatalf("event %d: msg still too long (%d)", i, len(ev.Msg))
		}
		blob, _ := ev.Fields["blob"].(string)
		if blob == "" || !HasAPITruncationMarker(blob) {
			t.Fatalf("event %d: fields.blob not truncated", i)
		}
	}
}

func TestMarshalLogsPageSmallEventsUnmarked(t *testing.T) {
	page := PageResult{
		HeadCursor: 10,
		Events: []Event{{
			TS:     "2026-10-22T12:00:00Z",
			Source: "server",
			Level:  "info",
			Msg:    "ok",
		}},
	}
	body, err := MarshalLogsPage("/tmp/events.jsonl", page)
	if err != nil {
		t.Fatal(err)
	}
	var resp LogsPageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ResponseTruncated || len(resp.Events) != 1 || resp.Events[0].Truncated {
		t.Fatalf("unexpected truncation: %+v", resp)
	}
}
