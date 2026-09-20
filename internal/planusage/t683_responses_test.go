// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T683: the payload survives the parse, so a question about what the
// vendor said is answered from disk rather than from another request.

const t683Body = `{"five_hour":{"utilization":6,"resets_at":"2026-09-20T18:00:00Z"},` +
	`"seven_day":{"utilization":66,"resets_at":"2026-09-26T00:00:00Z"},` +
	`"limits":[{"kind":"weekly_scoped","scope":{"model":{"display_name":"Fable"}},"utilization":100}]}`

func t683Store(t *testing.T) *ReadingStore {
	t.Helper()
	st, err := OpenReadingStore(filepath.Join(t.TempDir(), "readings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func t683At(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The whole point: a field this product does not parse is still readable
// afterwards. The per-model window below is exactly the shape that went
// unnoticed for weeks.
func TestT683UnmappedFieldsSurviveInTheStoredPayload(t *testing.T) {
	st := t683Store(t)
	at := t683At(t, "2026-09-20T12:00:00Z")
	if err := st.AppendResponses(responsesFromReadings([]claudia.PlanUsage{{
		Provider:   claudia.ProviderClaude,
		Status:     claudia.PlanUsageAvailable,
		FetchedAt:  at,
		HTTPStatus: 200,
		RawBody:    t683Body,
	}}, at)); err != nil {
		t.Fatal(err)
	}
	got, err := st.Responses("claude", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("stored %d payloads, want 1", len(got))
	}
	if got[0].Body != t683Body {
		t.Fatalf("payload changed in the round trip:\n got %q\nwant %q", got[0].Body, t683Body)
	}
	if !strings.Contains(got[0].Body, `"display_name":"Fable"`) {
		t.Fatal("the per-model window this table exists for did not survive")
	}
	if got[0].HTTPStatus != 200 || got[0].Status != string(claudia.PlanUsageAvailable) {
		t.Fatalf("status lost: http=%d status=%q", got[0].HTTPStatus, got[0].Status)
	}
	if !got[0].FetchedAt.Equal(at) {
		t.Fatalf("FetchedAt = %s, want %s", got[0].FetchedAt, at)
	}
}

// A refusal is worth keeping too: it is the evidence for why a gauge
// stopped moving, and it is what a rate-limit post-mortem needs.
func TestT683RefusalsAreKeptWithTheirReason(t *testing.T) {
	st := t683Store(t)
	at := t683At(t, "2026-09-20T12:00:00Z")
	body := `{"type":"error","error":{"type":"rate_limit_error"}}`
	if err := st.AppendResponses([]Response{{
		Provider: "claude", FetchedAt: at, HTTPStatus: 429,
		Status: string(claudia.PlanUsageUnavailable),
		Reason: "Claude usage HTTP 429", Body: body,
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Responses("claude", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].HTTPStatus != 429 {
		t.Fatalf("the refusal was not kept: %+v", got)
	}
	if !strings.Contains(got[0].Reason, "429") {
		t.Fatalf("reason lost: %q", got[0].Reason)
	}
}

// The table is bounded per provider and one provider's history never
// evicts another's.
func TestT683HistoryIsBoundedPerProvider(t *testing.T) {
	st := t683Store(t)
	base := t683At(t, "2026-09-20T00:00:00Z")
	for i := range ResponsesPerProvider + 20 {
		if err := st.AppendResponses([]Response{{
			Provider: "claude", FetchedAt: base.Add(time.Duration(i) * time.Minute),
			HTTPStatus: 200, Status: "available", Body: `{"n":` + itoa(i) + `}`,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AppendResponses([]Response{{
		Provider: "grok", FetchedAt: base, HTTPStatus: 200, Status: "available", Body: `{"grok":1}`,
	}}); err != nil {
		t.Fatal(err)
	}
	claude, err := st.Responses("claude", ResponsesPerProvider)
	if err != nil {
		t.Fatal(err)
	}
	if len(claude) != ResponsesPerProvider {
		t.Fatalf("kept %d claude payloads, want the %d cap", len(claude), ResponsesPerProvider)
	}
	// Newest first, and the newest is the last one written.
	if !strings.Contains(claude[0].Body, itoa(ResponsesPerProvider+19)) {
		t.Fatalf("newest payload is %q, want the last written", claude[0].Body)
	}
	grok, err := st.Responses("grok", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(grok) != 1 {
		t.Fatalf("trimming claude took grok's payload with it: %d rows", len(grok))
	}
}

// A reading with no body writes no row: an empty payload is not evidence,
// and rows of nothing would push real ones out of a bounded table.
func TestT683BodylessReadingsWriteNothing(t *testing.T) {
	st := t683Store(t)
	at := t683At(t, "2026-09-20T12:00:00Z")
	rs := responsesFromReadings([]claudia.PlanUsage{
		{Provider: claudia.ProviderBedrock, Status: claudia.PlanUsageUnavailable, FetchedAt: at},
		{Provider: claudia.ProviderClaude, Status: claudia.PlanUsageAvailable, FetchedAt: at, RawBody: "   "},
	}, at)
	// Both are bodyless: one never had a payload, the other has only
	// whitespace. Neither is evidence, so neither becomes a row.
	if len(rs) != 0 {
		t.Fatalf("responsesFromReadings kept %d rows from bodyless readings, want 0", len(rs))
	}
	if err := st.AppendResponses(rs); err != nil {
		t.Fatal(err)
	}
	got, err := st.Responses("claude", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a whitespace body was stored as evidence: %+v", got)
	}
}

// An existing readings database gains the table on open, without losing
// the samples already in it.
func TestT683ExistingDatabaseGainsTheTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.db")
	st, err := OpenReadingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	resets := t683At(t, "2026-09-26T00:00:00Z")
	if err := st.Append([]Reading{{
		Provider: "claude", Window: WindowWeekly,
		FetchedAt: t683At(t, "2026-09-20T12:00:00Z"), Remaining: 34, ResetsAt: &resets,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := OpenReadingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	pts, err := again.Series("claude", WindowWeekly, &resets)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("reopening lost the samples: %d points", len(pts))
	}
	if err := again.AppendResponses([]Response{{
		Provider: "claude", FetchedAt: time.Now(), HTTPStatus: 200,
		Status: "available", Body: t683Body,
	}}); err != nil {
		t.Fatalf("the table was not created on open: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
