// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/rsi"
)

func rsiDispositionsServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	state := t.TempDir()
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, state
}

func getRSIJudgments(t *testing.T, url string) rsiJudgmentsResponse {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status %d", url, resp.StatusCode)
	}
	var out rsiJudgmentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// 🎯T354 oracle: an empty store is an honest empty list, not an error.
func TestRSIDispositionsEmptyStoreIsHonest(t *testing.T) {
	srv, _ := rsiDispositionsServer(t)
	got := getRSIJudgments(t, srv.URL+"/api/rsi/dispositions")
	if got.Count != 0 || got.Total != 0 || got.Pending != 0 {
		t.Fatalf("empty store not honest: %+v", got)
	}
	if got.Judgments == nil {
		t.Fatal("judgments must serialize as [] not null")
	}
	if len(got.Judgments) != 0 {
		t.Fatalf("judgments=%+v", got.Judgments)
	}
}

// 🎯T354 oracle: pending + ignored + filed shapes all reach the owner list with
// severity, delivered_at, fingerprint, evidence summary, reason and target_id.
func TestRSIDispositionsPendingIgnoredFiledShapes(t *testing.T) {
	srv, state := rsiDispositionsServer(t)
	store, err := rsi.OpenDispositionStore(state)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	err = store.RecordDelivered([]rsi.Judgment{
		{
			Fingerprint: "fp-pending",
			Name:        "chat gap",
			Observation: "owner asked twice before a reply landed",
			Severity:    "medium",
			Evidence: []rsi.EvidenceRef{
				{Source: "owner_chat", SourceID: "chatlog-2026-08-09", Kind: "chat_gap"},
			},
		},
		{
			Fingerprint: "fp-ignored",
			Name:        "phrase friction",
			Observation: "repeat phrasing in overseer replies",
			Severity:    "low",
		},
		{
			Fingerprint: "fp-filed",
			Name:        "repair churn",
			Observation: "three follow-up commits on the same file",
			Severity:    "high",
			Mode:        rsi.RetroModeLabel,
		},
	}, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetDisposition(rsi.SetDispositionArgs{
		Fingerprint: "fp-ignored",
		Disposition: rsi.DispositionIgnoreWithReason,
		Reason:      "one-off, no standing pattern",
		Now:         base.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetDisposition(rsi.SetDispositionArgs{
		Fingerprint: "fp-filed",
		Disposition: rsi.DispositionFile,
		TargetID:    "T999",
		TargetCwd:   state,
		Now:         base.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got := getRSIJudgments(t, srv.URL+"/api/rsi/dispositions")
	if got.Total != 3 || got.Count != 3 {
		t.Fatalf("counts total=%d count=%d", got.Total, got.Count)
	}
	if got.Pending != 1 {
		t.Fatalf("pending=%d, want 1", got.Pending)
	}
	byFP := map[string]rsi.DispositionEntry{}
	for _, e := range got.Judgments {
		byFP[e.Fingerprint] = e
	}

	pending, ok := byFP["fp-pending"]
	if !ok {
		t.Fatal("pending judgment missing from owner list")
	}
	if pending.Disposition != rsi.DispositionPending {
		t.Fatalf("pending disposition=%q", pending.Disposition)
	}
	if pending.Severity != "medium" || pending.Observation == "" || pending.DeliveredAt.IsZero() {
		t.Fatalf("pending row incomplete: %+v", pending)
	}
	if pending.Evidence != "owner_chat:chatlog-2026-08-09 (chat_gap)" {
		t.Fatalf("evidence summary=%q", pending.Evidence)
	}

	ignored := byFP["fp-ignored"]
	if ignored.Disposition != rsi.DispositionIgnoreWithReason {
		t.Fatalf("ignored disposition=%q", ignored.Disposition)
	}
	if ignored.Reason != "one-off, no standing pattern" {
		t.Fatalf("ignored reason=%q", ignored.Reason)
	}

	filed := byFP["fp-filed"]
	if filed.Disposition != rsi.DispositionFile || filed.TargetID != "T999" {
		t.Fatalf("filed row=%+v", filed)
	}
	if filed.Mode != rsi.RetroModeLabel {
		t.Fatalf("retro provenance lost: mode=%q", filed.Mode)
	}

	// Filter narrows to one bucket without losing the global pending count.
	only := getRSIJudgments(t, srv.URL+"/api/rsi/dispositions?disposition=pending")
	if only.Count != 1 || only.Judgments[0].Fingerprint != "fp-pending" {
		t.Fatalf("filtered list=%+v", only)
	}
	if only.Total != 3 || only.Pending != 1 {
		t.Fatalf("filtered totals total=%d pending=%d", only.Total, only.Pending)
	}
}

// 🎯T354: an unknown filter is a 400, not a silently empty list (which would
// read as "the coach found nothing").
func TestRSIDispositionsRejectsUnknownFilter(t *testing.T) {
	srv, _ := rsiDispositionsServer(t)
	resp, err := http.Get(srv.URL + "/api/rsi/dispositions?disposition=bogus")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}
