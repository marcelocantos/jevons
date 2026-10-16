// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeltaNewAndUpdated(t *testing.T) {
	st := RepoState{Repo: "o/r", LastSeen: map[string]string{}}
	items := []Item{
		{Kind: KindIssue, Number: 1, Title: "a", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Kind: KindPR, Number: 2, Title: "b", UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	fresh, next := Delta(st, items)
	if len(fresh) != 2 {
		t.Fatalf("first poll: %d", len(fresh))
	}
	// Same timestamps → nothing fresh.
	fresh, next = Delta(next, items)
	if len(fresh) != 0 {
		t.Fatalf("unchanged: %d", len(fresh))
	}
	// Bump one.
	items[0].UpdatedAt = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	fresh, _ = Delta(next, items)
	if len(fresh) != 1 || fresh[0].Number != 1 {
		t.Fatalf("updated issue: %+v", fresh)
	}
}

func TestRunOnceHermeticFakeGh(t *testing.T) {
	dir := t.TempDir()
	issueJSON, _ := json.Marshal([]map[string]any{
		{
			"number": 7, "title": "typo in README", "url": "https://github.com/acme/r/issues/7",
			"state": "OPEN", "updatedAt": "2026-10-14T10:00:00Z",
			"author": map[string]string{"login": "alice"},
			"labels": []map[string]string{},
		},
	})
	prJSON, _ := json.Marshal([]map[string]any{
		{
			"number": 197, "title": "big external PR", "url": "https://github.com/acme/r/pull/197",
			"state": "OPEN", "isDraft": false, "updatedAt": "2026-10-14T11:00:00Z",
			"author": map[string]string{"login": "outsider"},
			"labels": []map[string]string{},
			"additions": 5000, "deletions": 10, "changedFiles": 23,
			"statusCheckRollup": []map[string]string{{"state": "SUCCESS"}},
		},
	})
	fake := &FakeRunner{Responses: map[string][]byte{
		"repo view --json nameWithOwner -q .nameWithOwner": []byte("acme/r\n"),
		"issue list": issueJSON,
		"pr list":    prJSON,
	}}
	var delivered []string
	var mu sync.Mutex
	agent, err := New(Args{
		StateDir: dir,
		Runner:   fake,
		Seats: FuncSeatLister(func() []Seat {
			return []Seat{{Name: "acme-po", WorkDir: "/tmp/acme", Role: "product-owner"}}
		}),
		Deliverer: briefCatcher{fn: func(po, text string) error {
			mu.Lock()
			delivered = append(delivered, po+"|"+text)
			mu.Unlock()
			return nil
		}},
		Now: func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := agent.RunOnce(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if res.FreshCount() != 2 {
		t.Fatalf("fresh=%d repos=%+v", res.FreshCount(), res.Repos)
	}
	if len(res.Repos) != 1 || !res.Repos[0].Delivered {
		t.Fatalf("repo result: %+v", res.Repos)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != 1 {
		t.Fatalf("delivered=%d", len(delivered))
	}
	if !strings.Contains(delivered[0], "jevons: kind repo-watch") {
		t.Fatalf("brief missing typed marker: %s", delivered[0])
	}
	if !strings.Contains(delivered[0], "acme-po") {
		t.Fatalf("brief missing po: %s", delivered[0])
	}
	// Second cycle: no new items → no re-delivery.
	res2, err := agent.RunOnce(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if res2.FreshCount() != 0 {
		t.Fatalf("second cycle fresh=%d", res2.FreshCount())
	}
	if len(delivered) != 1 {
		t.Fatalf("re-delivered unchanged items: %d", len(delivered))
	}
}

type briefCatcher struct {
	fn func(po, text string) error
}

func (b briefCatcher) DeliverRepoWatch(po, text string) error { return b.fn(po, text) }

func TestDiscoverTargetsConfigAndSeats(t *testing.T) {
	off := false
	cfg := Config{
		Repos: []RepoConfig{
			{Repo: "acme/r", Workdir: "/w", PO: "acme-po"},
			{Repo: "skip/me", Enabled: &off, PO: "skip-po", Workdir: "/s"},
		},
	}
	seats := []Seat{
		{Name: "acme-po", WorkDir: "/w2"}, // already covered by config
		{Name: "mnemo-po", WorkDir: "/m"},
		{Name: "jv-t1-worker", WorkDir: "/x"}, // not a PO
	}
	got := DiscoverTargets(cfg, seats)
	if len(got) != 3 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	// skip/me present but disabled
	var foundMnemo, foundSkip bool
	for _, t := range got {
		if t.PO == "mnemo-po" && t.Workdir == "/m" {
			foundMnemo = true
		}
		if t.Repo == "skip/me" && !t.Enabled {
			foundSkip = true
		}
	}
	if !foundMnemo || !foundSkip {
		t.Fatalf("targets: %+v", got)
	}
}

func TestParseListAndCI(t *testing.T) {
	body := []byte(`[
	  {"number":1,"title":"x","url":"u","state":"OPEN","isDraft":false,
	   "updatedAt":"2026-01-01T00:00:00Z","author":{"login":"dependabot[bot]"},
	   "labels":[],"additions":1,"deletions":1,"changedFiles":1,
	   "statusCheckRollup":[{"state":"SUCCESS"},{"state":"FAILURE"}]}
	]`)
	items, err := parseList(body, KindPR)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].IsBot || items[0].CIState != "failure" {
		t.Fatalf("%+v", items[0])
	}
}

func TestFormatBriefMarkers(t *testing.T) {
	s := FormatBrief(Brief{
		Repo: "a/b", PO: "a-po", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Items: []Item{{Kind: KindIssue, Number: 1, Title: "hi", URL: "http://x", Author: "u",
			UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}},
	})
	for _, want := range []string{
		"jevons: kind repo-watch",
		"jevons: repo a/b",
		"jevons: po a-po",
		"issue #1 hi",
		"jevons_repo_escalate",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}
