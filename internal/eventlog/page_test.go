// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePageFixture(t *testing.T, events ...Event) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var data strings.Builder
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		data.Write(line)
		data.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(data.String()), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPageStableAcrossAppendAndFilters(t *testing.T) {
	t1 := "2026-09-27T10:00:00Z"
	t2 := "2026-09-27T10:01:00Z"
	path := writePageFixture(t,
		Event{TS: t1, Source: "server", Level: "info", Component: "route", Decision: "send", Msg: "sent alpha"},
		Event{TS: t1, Source: "browser", Level: "debug", Component: "history", Msg: "hydrate page"},
		Event{TS: t2, Source: "server", Level: "warn", Component: "route", Decision: "retry", Msg: "retry beta"},
	)
	query := Query{Source: "server", Component: "route"}
	page, err := Page(path, nil, 1, query)
	if err != nil || len(page.Events) != 1 || page.Events[0].Msg != "retry beta" || page.NextCursor == nil {
		t.Fatalf("first page=%+v err=%v", page, err)
	}
	if err := os.WriteFile(path, append(mustRead(t, path), []byte(`{"ts":"2026-09-27T10:02:00Z","source":"server","level":"info","msg":"later"}`+"\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	older, err := Page(path, page.NextCursor, 1, query)
	if err != nil || len(older.Events) != 1 || older.Events[0].Msg != "sent alpha" || older.NextCursor != nil {
		t.Fatalf("older page=%+v err=%v", older, err)
	}
	filtered, err := Page(path, nil, 10, Query{Source: "server", Level: "warn", Decision: "retry", Contains: "BETA", Since: time.Date(2026, 9, 27, 10, 0, 30, 0, time.UTC)})
	if err != nil || len(filtered.Events) != 1 || filtered.Events[0].Msg != "retry beta" {
		t.Fatalf("filtered page=%+v err=%v", filtered, err)
	}
	browser, err := Page(path, nil, 10, Query{Source: "browser", Level: "debug"})
	if err != nil || len(browser.Events) != 1 || browser.Events[0].Msg != "hydrate page" {
		t.Fatalf("browser page=%+v err=%v", browser, err)
	}
}

func TestPageScanBudgetMakesProgressAcrossChatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	server, _ := json.Marshal(Event{TS: "2026-09-27T10:00:00Z", Source: "server", Level: "info", Msg: "important decision"})
	browser, _ := json.Marshal(Event{TS: "2026-09-27T10:01:00Z", Source: "browser", Level: "debug", Msg: strings.Repeat("hydrate", 30)})
	data := append(append([]byte{}, server...), '\n')
	for len(data) < MaxPageScanBytes+1<<20 {
		data = append(data, browser...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var before *int64
	var found bool
	for attempt := 0; attempt < 3; attempt++ {
		page, err := Page(path, before, 100, Query{Source: "server"})
		if err != nil || page.ScannedBytes > MaxPageScanBytes {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		if len(page.Events) > 0 {
			found = page.Events[0].Msg == "important decision"
			break
		}
		if page.NextCursor == nil || before != nil && *page.NextCursor >= *before {
			t.Fatalf("cursor did not advance: %+v", page)
		}
		before = page.NextCursor
	}
	if !found {
		t.Fatal("server event lost behind browser chatter")
	}
}

func TestReadAfterOnlyCompleteMatchingLines(t *testing.T) {
	path := writePageFixture(t, Event{TS: "2026-09-27T10:00:00Z", Source: "browser", Level: "debug", Msg: "hydrate"})
	start := int64(len(mustRead(t, path)))
	fragment := []byte(`{"ts":"2026-09-27T10:00:01Z","source":"server","level":"info","msg":"new"}`)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(fragment); err != nil {
		t.Fatal(err)
	}
	part, err := ReadAfter(path, start, 10, Query{Source: "server"})
	if err != nil || len(part.Events) != 0 || part.NextCursor != start {
		t.Fatalf("partial=%+v err=%v", part, err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	complete, err := ReadAfter(path, start, 10, Query{Source: "server"})
	if err != nil || len(complete.Events) != 1 || complete.Events[0].Event.Msg != "new" || complete.Events[0].Cursor != complete.NextCursor {
		t.Fatalf("complete=%+v err=%v", complete, err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
