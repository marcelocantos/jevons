// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestionview

import (
	"testing"
	"time"
)

// 🎯T404.2: ResolveSilence retires an open, default-carrying question once
// it has sat past the window, and leaves a question with no default alone,
// whatever its age.
func TestT4042ResolveSilence(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	withDefault := Question{Identity: Identity{Repo: dir, Target: "T404.2", ID: "a", Version: "1"}, Text: "q1", Asker: "jv-a", AnswerRoute: "jv-a", Default: "blue"}
	noDefault := Question{Identity: Identity{Repo: dir, Target: "T404.2", ID: "b", Version: "1"}, Text: "q2", Asker: "jv-b", AnswerRoute: "jv-b"}
	if err := st.Record(withDefault); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(noDefault); err != nil {
		t.Fatal(err)
	}

	// Too soon: nothing resolves.
	resolved, err := st.ResolveSilence(time.Now(), time.Hour)
	if err != nil || len(resolved) != 0 {
		t.Fatalf("resolved too early: %+v %v", resolved, err)
	}

	// Past the window: only the defaulted one resolves.
	resolved, err = st.ResolveSilence(time.Now().Add(2*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Identity.ID != "a" || resolved[0].Default != "blue" {
		t.Fatalf("resolved = %+v", resolved)
	}

	rows, err := st.List(false)
	if err != nil {
		t.Fatal(err)
	}
	var a, b Question
	for _, r := range rows {
		switch r.Identity.ID {
		case "a":
			a = r
		case "b":
			b = r
		}
	}
	if a.State != Moot || a.Resolution != "moot: proceeded on default" {
		t.Fatalf("defaulted question: %+v", a)
	}
	if b.State != Open {
		t.Fatalf("no-default question should stay open: %+v", b)
	}

	// A non-positive window is refused rather than silently resolving
	// everything.
	if _, err := st.ResolveSilence(time.Now(), 0); err == nil {
		t.Fatal("zero window should be refused")
	}
}
