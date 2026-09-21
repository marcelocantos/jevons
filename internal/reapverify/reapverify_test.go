// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package reapverify

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeTargetID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"T753":  "T753",
		"t753":  "T753",
		"🎯T753": "T753",
		"753":   "T753",
		"":      "",
	}
	for in, want := range cases {
		if got := NormalizeTargetID(in); got != want {
			t.Errorf("NormalizeTargetID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreRecordAndPendingFor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })

	p := Pending{
		TargetID:   "T753",
		Seat:       "jv-t753-worker",
		Owes:       "jevons-po",
		Repo:       "/work/jevons",
		ReapReason: "finished_work",
		Commits:    []Commit{{SHA: "abc1234", Subject: "fix(T753): landed"}},
	}
	if err := st.Record(p); err != nil {
		t.Fatal(err)
	}
	got, ok := st.PendingFor("T753", "/work/jevons")
	if !ok {
		t.Fatal("PendingFor missed the record")
	}
	if got.Seat != p.Seat || len(got.Commits) != 1 {
		t.Fatalf("got = %+v", got)
	}
	if _, ok := st.PendingFor("T753", "/other/repo"); ok {
		t.Fatal("wrong repo matched")
	}
	if _, ok := st.PendingFor("T753", ""); !ok {
		t.Fatal("empty repo should match any record for the target")
	}
}

func TestStoreRefusesEmptyCommits(t *testing.T) {
	t.Parallel()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = st.Record(Pending{TargetID: "T753", Commits: nil})
	if err == nil {
		t.Fatal("expected refuse with no commits")
	}
}

func TestStoreResolveAndNoSilentExpiry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return start })
	if err := st.Record(Pending{
		TargetID: "T753",
		Repo:     dir,
		Commits:  []Commit{{SHA: "deadbeef", Subject: "fix(T753): x"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Resolve("T753", dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.PendingFor("T753", dir); ok {
		t.Fatal("record survived resolve")
	}

	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	late := start.Add(8 * 24 * time.Hour)
	st2.SetClock(func() time.Time { return late })
	if err := st2.Record(Pending{
		TargetID: "T999",
		Repo:     dir,
		Commits:  []Commit{{SHA: "cafebabe", Subject: "fix(T999): old"}},
	}); err != nil {
		t.Fatal(err)
	}
	// Force-write an expired record by reopening with clock at start+8d
	st2.SetClock(func() time.Time { return late })
	if list := st2.List(); len(list) != 1 || list[0].TargetID != "T999" {
		t.Fatalf("list = %+v", list)
	}
	st2.SetClock(func() time.Time { return late.Add(8 * 24 * time.Hour) })
	if list := st2.List(); len(list) != 1 {
		t.Fatalf("pending decision silently expired: %+v", list)
	}
}

func TestStorePersistsAcrossOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Record(Pending{
		TargetID: "T753",
		Repo:     dir,
		Seat:     "jv-t753",
		Commits:  []Commit{{SHA: "1111111", Subject: "fix(T753): y"}},
	}); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st2.PendingFor("T753", dir); !ok {
		t.Fatal("record not durable across reopen")
	}
	if _, err := filepath.Glob(filepath.Join(dir, "fleet", StoreFileName)); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsMalformedState(t *testing.T) {
	for _, data := range []string{"", "null", "{}", "{broken", `{"pending":{"wrong":{"target_id":"T1"}}}`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := Open(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "fleet", StoreFileName), []byte(data), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir); err == nil {
				t.Fatal("malformed state silently accepted")
			}
		})
	}
}
