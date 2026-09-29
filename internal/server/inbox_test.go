// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/notice"
)

// 🎯T254.4 oracle: a recorded terminal notice is queryable over HTTP, both in
// its parent PO's inbox and in the overseer's fleet-wide view.
func TestInboxListHTTP(t *testing.T) {
	state := t.TempDir()
	base := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	for i, r := range []struct{ agent, parent, text string }{
		{"jv-a", "jevons-po", "```jevons\njevons: kind finish-report\njevons: target T1\njevons: sha abc\njevons: verdict GREEN\njevons: silent-ledger none\n```\n\nLanded.\n"},
		{"jv-b", "other-po", "```jevons\njevons: kind finish-report\njevons: target T2\njevons: silent-ledger none\n```\n\nBlocked on T3.\n"},
		{"jv-c", "", "```jevons\njevons: kind escalation\njevons: target T4\n```\n\nNeeds design: owner call.\n"},
	} {
		n, ok := notice.FromReport(r.agent, r.parent, r.text, base.Add(time.Duration(i)*time.Minute))
		if !ok {
			t.Fatalf("fixture %s not a terminal report", r.agent)
		}
		if err := notice.Append(state, n); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	get := func(query string) (int, inboxListResponse) {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/inbox" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out inboxListResponse
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, out
	}

	code, all := get("")
	if code != http.StatusOK || all.Count != 3 {
		t.Fatalf("fleet-wide: status %d count %d, want 200/3: %+v", code, all.Count, all)
	}
	wantOutcomes := []notice.Outcome{notice.OutcomeDone, notice.OutcomeBlocked, notice.OutcomeNeedsDesign}
	for i, n := range all.Notices {
		if n.Outcome != wantOutcomes[i] {
			t.Fatalf("notice %d (%s) outcome %q, want %q", i, n.Agent, n.Outcome, wantOutcomes[i])
		}
	}

	code, po := get("?parent=jevons-po")
	if code != http.StatusOK || po.Count != 1 || po.Notices[0].Agent != "jv-a" || po.Notices[0].SHA != "abc" {
		t.Fatalf("parent inbox: status %d %+v", code, po)
	}

	code, blocked := get("?outcome=blocked")
	if code != http.StatusOK || blocked.Count != 1 || blocked.Notices[0].Agent != "jv-b" {
		t.Fatalf("outcome filter: status %d %+v", code, blocked)
	}

	code, latest := get("?limit=1")
	if code != http.StatusOK || latest.Count != 1 || latest.Notices[0].Agent != "jv-c" {
		t.Fatalf("limit: status %d %+v", code, latest)
	}

	if code, _ := get("?outcome=finished"); code != http.StatusBadRequest {
		t.Fatalf("unknown outcome status %d, want 400", code)
	}

	code, empty := get("?parent=nobody")
	if code != http.StatusOK || empty.Count != 0 || empty.Notices == nil {
		t.Fatalf("empty inbox: status %d %+v (notices must be [], not null)", code, empty)
	}
}
