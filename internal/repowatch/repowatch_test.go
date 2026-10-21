// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeRunner is the hermetic `gh` double (no live GitHub calls — acceptance
// #4). It answers by (dir, subcommand) key, where subcommand is args[0:2]
// joined ("issue list" / "pr list").
type fakeRunner struct {
	calls   int
	byDir   map[string]map[string][]byte
	failing map[string]error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{byDir: map[string]map[string][]byte{}, failing: map[string]error{}}
}

func (f *fakeRunner) set(dir, subcmd string, payload []byte) {
	m := f.byDir[dir]
	if m == nil {
		m = map[string][]byte{}
		f.byDir[dir] = m
	}
	m[subcmd] = payload
}

func (f *fakeRunner) fail(dir, subcmd string, err error) {
	f.failing[dir+"\x00"+subcmd] = err
}

func (f *fakeRunner) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	f.calls++
	sub := strings.Join(args[:2], " ")
	if err, ok := f.failing[dir+"\x00"+sub]; ok {
		return nil, err
	}
	if m, ok := f.byDir[dir]; ok {
		if payload, ok := m[sub]; ok {
			return payload, nil
		}
	}
	return []byte("[]"), nil
}

func issuesJSON(items ...ghIssueJSON) []byte {
	b, _ := json.Marshal(items)
	return b
}

func prsJSON(items ...ghPRJSON) []byte {
	b, _ := json.Marshal(items)
	return b
}

func TestListIssuesParsesFields(t *testing.T) {
	r := newFakeRunner()
	r.set("/repo", "issue list", issuesJSON(ghIssueJSON{
		Number: 42, Title: "bug", URL: "https://x/42", State: "OPEN",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Author:    struct{ Login string `json:"login"` }{Login: "alice"},
	}))
	items, err := ListIssues(context.Background(), r, "/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(items) != 1 || items[0].Number != 42 || items[0].Kind != "issue" || items[0].Author != "alice" {
		t.Fatalf("items=%+v", items)
	}
	if items[0].State != "open" {
		t.Fatalf("state=%q, want lowercased", items[0].State)
	}
}

func TestListPRsParsesCIAndSize(t *testing.T) {
	r := newFakeRunner()
	r.set("/repo", "pr list", prsJSON(ghPRJSON{
		Number: 7, Title: "feat", URL: "https://x/7", State: "OPEN",
		UpdatedAt: "2026-01-02T00:00:00Z", IsDraft: false,
		Additions: 10, Deletions: 2,
		Author: struct{ Login string `json:"login"` }{Login: "bob"},
		Files: []struct {
			Path string `json:"path"`
		}{{Path: "a.go"}, {Path: "b.go"}},
		StatusCheckRollup: []struct {
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		}{{Conclusion: "SUCCESS"}},
	}))
	items, err := ListPRs(context.Background(), r, "/repo")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(items) != 1 || items[0].Files != 2 || items[0].CIState != "green" {
		t.Fatalf("items=%+v", items)
	}
}

func TestPRCIStateFailingBeatsGreen(t *testing.T) {
	got := prCIState([]struct {
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	}{{Conclusion: "SUCCESS"}, {Conclusion: "FAILURE"}})
	if got != "failing" {
		t.Fatalf("ci state=%q, want failing", got)
	}
}

type recordingDeliverer struct {
	sent []struct{ target, text string }
	err  error
}

func (d *recordingDeliverer) Deliver(target, text string) error {
	if d.err != nil {
		return d.err
	}
	d.sent = append(d.sent, struct{ target, text string }{target, text})
	return nil
}

func TestRunOnceDeliversOnlyNewItemsAndAdvancesWatermark(t *testing.T) {
	dir := t.TempDir()
	r := newFakeRunner()
	r.set("/repo-a", "issue list", issuesJSON(ghIssueJSON{
		Number: 1, Title: "first", URL: "u1", State: "OPEN",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}))
	r.set("/repo-a", "pr list", prsJSON())
	deliverer := &recordingDeliverer{}
	a, err := New(Args{
		StateDir:  dir,
		Repos:     []RepoSpec{{Name: "repo-a", Dir: "/repo-a", Owner: "repo-a-po"}},
		Runner:    r,
		Deliverer: deliverer,
		Now:       func() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := SaveConfig(dir, DefaultConfig()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	res, err := a.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !res.Changed() {
		t.Fatalf("expected changed cycle: %+v", res)
	}
	if len(deliverer.sent) != 1 || deliverer.sent[0].target != "repo-a-po" {
		t.Fatalf("delivered=%+v", deliverer.sent)
	}
	if !strings.Contains(deliverer.sent[0].text, "first") {
		t.Fatalf("brief missing item title: %q", deliverer.sent[0].text)
	}

	// Second cycle with no new items: watermark already covers item #1, so
	// nothing is delivered again.
	res2, err := a.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce 2: %v", err)
	}
	if res2.Changed() {
		t.Fatalf("expected no change on second pass: %+v", res2)
	}
	if len(deliverer.sent) != 1 {
		t.Fatalf("expected no re-delivery, got %d sends", len(deliverer.sent))
	}

	st, err := LoadState(dir, "repo-a")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !st.Watermark.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("watermark=%v", st.Watermark)
	}
}

func TestRunOnceDeliversUpdatedItemPastWatermark(t *testing.T) {
	dir := t.TempDir()
	if err := SaveState(dir, RepoState{Repo: "repo-a", Watermark: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := SaveConfig(dir, DefaultConfig()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	r := newFakeRunner()
	r.set("/repo-a", "issue list", issuesJSON(ghIssueJSON{
		Number: 1, Title: "first (updated)", URL: "u1", State: "CLOSED",
		UpdatedAt: "2026-01-03T00:00:00Z",
	}))
	r.set("/repo-a", "pr list", prsJSON())
	deliverer := &recordingDeliverer{}
	a, err := New(Args{
		StateDir:  dir,
		Repos:     []RepoSpec{{Name: "repo-a", Dir: "/repo-a", Owner: "repo-a-po"}},
		Runner:    r,
		Deliverer: deliverer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !res.Changed() || len(deliverer.sent) != 1 {
		t.Fatalf("expected one delivery for the updated item: %+v / %+v", res, deliverer.sent)
	}
}

func TestRunOnceSkipsDisabledRepo(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DisabledRepos = []string{"repo-b"}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	r := newFakeRunner()
	deliverer := &recordingDeliverer{}
	a, err := New(Args{
		StateDir:  dir,
		Repos:     []RepoSpec{{Name: "repo-b", Dir: "/repo-b", Owner: "repo-b-po"}},
		Runner:    r,
		Deliverer: deliverer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if r.calls != 0 {
		t.Fatalf("expected no gh calls for an opted-out repo, got %d", r.calls)
	}
	if len(res.Repos) != 1 || res.Repos[0].Skipped == "" {
		t.Fatalf("expected a skip record: %+v", res.Repos)
	}
}

func TestRunOnceDisabledGloballySkipsEverything(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = false
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	r := newFakeRunner()
	a, err := New(Args{StateDir: dir, Repos: []RepoSpec{{Name: "repo-a", Dir: "/repo-a"}}, Runner: r})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if r.calls != 0 || res.Changed() {
		t.Fatalf("expected no work when globally disabled: calls=%d res=%+v", r.calls, res)
	}
}

func TestRunOnceReportsGhErrorsWithoutLosingOtherRepos(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, DefaultConfig()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	r := newFakeRunner()
	r.fail("/broken", "issue list", fmt.Errorf("gh: not found"))
	r.set("/ok", "issue list", issuesJSON(ghIssueJSON{Number: 1, Title: "ok", UpdatedAt: "2026-01-01T00:00:00Z"}))
	r.set("/ok", "pr list", prsJSON())
	a, err := New(Args{
		StateDir: dir,
		Repos: []RepoSpec{
			{Name: "broken", Dir: "/broken"},
			{Name: "ok", Dir: "/ok", Owner: "ok-po"},
		},
		Runner: r,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := a.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(res.Repos) != 2 {
		t.Fatalf("expected both repos reported: %+v", res.Repos)
	}
	var sawBroken, sawOK bool
	for _, rc := range res.Repos {
		if rc.Repo == "broken" {
			sawBroken = rc.Skipped != ""
		}
		if rc.Repo == "ok" {
			sawOK = len(rc.New) == 1
		}
	}
	if !sawBroken || !sawOK {
		t.Fatalf("expected broken skipped and ok scanned: %+v", res.Repos)
	}
}

func TestConfigPatchApply(t *testing.T) {
	cfg := DefaultConfig()
	enabled := false
	patch := ConfigPatch{Enabled: &enabled, AddDisabled: "foo", UpdatedBy: "jevons"}
	out := patch.Apply(cfg)
	if out.Enabled {
		t.Fatalf("expected disabled")
	}
	if !out.RepoDisabled("foo") {
		t.Fatalf("expected foo disabled: %+v", out.DisabledRepos)
	}
	if out.UpdatedBy != "jevons" || out.UpdatedAt == "" {
		t.Fatalf("expected provenance stamped: %+v", out)
	}
	patch2 := ConfigPatch{RemoveEnabled: "foo"}
	out2 := patch2.Apply(out)
	if out2.RepoDisabled("foo") {
		t.Fatalf("expected foo re-enabled: %+v", out2.DisabledRepos)
	}
}

func TestLoadConfigDefaultsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Enabled || cfg.IntervalOf() != DefaultInterval {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestRenderBriefNamesEachItem(t *testing.T) {
	brief := RenderBrief("jevons", []Item{
		{Kind: "issue", Number: 5, Title: "bug", State: "open", Author: "alice", URL: "u5"},
		{Kind: "pr", Number: 9, Title: "fix", State: "open", Author: "bob", URL: "u9", CIState: "green"},
	})
	if !strings.Contains(brief, "jevons") || !strings.Contains(brief, "#5") || !strings.Contains(brief, "#9") {
		t.Fatalf("brief=%q", brief)
	}
}
