// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestionview

import (
	"strings"
	"testing"
)

// 🎯T404: the durable view keeps the default and options across a reopen,
// and Find returns the exact version so an answer can be routed to its asker.
func TestT404StoreKeepsDefaultAndOptions(t *testing.T) {
	dir := t.TempDir()
	id := Identity{Repo: dir, Target: "T404", ID: "surface", Version: "1"}
	q := Question{Identity: id, Text: "Cockpit or Slack?", Asker: "jv-t404-auto", AnswerRoute: "jv-t404-auto", Default: "cockpit unless you say otherwise", Options: []string{"cockpit", "slack"}}
	if err := New(dir).Record(q); err != nil {
		t.Fatal(err)
	}
	got, err := New(dir).Find(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Default != q.Default || len(got.Options) != 2 || got.Options[0] != "cockpit" {
		t.Fatalf("default/options not persisted: %+v", got)
	}
	if !strings.HasPrefix(got.SilenceOutcome(), "proceeds on default") {
		t.Fatalf("silence outcome = %q", got.SilenceOutcome())
	}
	if _, err := New(dir).Find(Identity{Repo: dir, Target: "T404", ID: "surface", Version: "2"}); err == nil {
		t.Fatal("Find matched a version that was never recorded")
	}
}
