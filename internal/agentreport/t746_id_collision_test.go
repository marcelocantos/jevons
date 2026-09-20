// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agentreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// 🎯T746 — two reports written in the same second do not overwrite each other.
//
// NewID is timestamp(1s) + sha256(text)[:4], and the digest carries no
// per-report entropy when the text repeats: sha256("No response requested.")
// [:4] is 85caba2f, so every harness ack ever stored shares that suffix — 649
// of them in this machine's store at the time of writing, against 3848 files
// total. Save wrote <id>.json with a rename, and rename overwrites. Two
// reports with the same text in the same second therefore resolved to one
// file and the later one deleted the earlier.
//
// For a 22-byte ack that was harmless and invisible, which is precisely why it
// survived from 🎯T388's landing until 2026-09-21. For two real reports it
// silently lost one.
//
// THE 🎯T388 GUARANTEE THIS SERVES, stated here so the two cannot drift: every
// report an agent delivers is stored BEFORE delivery and before 🎯T165/T195
// auto-deregistration removes the agent, so the full text outlives its author
// and is retrievable afterwards by id, without consulting the registry.
// TestT746StoredReportOutlivesItsAuthor below asserts that guarantee against
// the same store this target changes. A store that silently drops a report is
// a store that does not make the guarantee, whatever its retrieval path does.

// t746At is one fixed second. Every save in these tests is stamped with it, so
// the one-second bucket is not a matter of test timing luck.
var t746At = time.Date(2026, 9, 20, 17, 17, 4, 0, time.UTC)

// saveOverwriting is the pre-🎯T746 store path, kept in its original shape:
// write a temp file beside the target and rename over whatever is there.
//
// It is here so the mutation is demonstrated rather than asserted. The test
// below runs it and the product path over identical input and shows this one
// losing a report — so if Save is ever reverted to this behaviour, the
// assertions in this file stop passing for a reason the file itself explains.
func saveOverwriting(t *testing.T, dir string, rec Record) {
	t.Helper()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, rec.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename %s: %v", path, err)
	}
}

func t746CountJSON(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			n++
		}
	}
	return n
}

func t746AgentDir(t *testing.T, state, agent string) string {
	t.Helper()
	dir, err := AgentDir(state, agent)
	if err != nil {
		t.Fatalf("AgentDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

// Acceptance clause 1: Save never writes two distinct reports to one file.
//
// The claim layer is exercised directly with two records that PROPOSE the same
// id but carry different bodies, because that is the shape the digest cannot
// rule out and the shape a rename destroys. The legacy path in the same test
// is the control: same input, one file, first body gone.
func TestT746TwoDistinctReportsNeverShareOneFile(t *testing.T) {
	const agent = "jv-t746-distinct"
	const forcedID = "20260920T171704Z-85caba2f"
	alpha := Record{ID: forcedID, Agent: agent, At: t746At, Text: "alpha: the first report", Bytes: 23}
	beta := Record{ID: forcedID, Agent: agent, At: t746At, Text: "beta: the second report", Bytes: 23}

	legacy := t746AgentDir(t, t.TempDir(), agent)
	saveOverwriting(t, legacy, alpha)
	saveOverwriting(t, legacy, beta)
	if n := t746CountJSON(t, legacy); n != 1 {
		t.Fatalf("control: legacy path wrote %d files, want 1 — the mutation is not the mutation", n)
	}

	state := t.TempDir()
	dir := t746AgentDir(t, state, agent)
	gotA, err := claimRecordFile(dir, alpha)
	if err != nil {
		t.Fatalf("claim alpha: %v", err)
	}
	gotB, err := claimRecordFile(dir, beta)
	if err != nil {
		t.Fatalf("claim beta: %v", err)
	}
	if gotA.ID == gotB.ID {
		t.Fatalf("both reports claimed id %s — one file, one survivor", gotA.ID)
	}
	if n := t746CountJSON(t, dir); n != 2 {
		t.Fatalf("product path wrote %d files, want 2", n)
	}
	for _, want := range []Record{gotA, gotB} {
		back, err := Load(state, agent, want.ID)
		if err != nil {
			t.Fatalf("load %s: %v", want.ID, err)
		}
		if back.Text != want.Text {
			t.Fatalf("report %s reads back %q, want %q", want.ID, back.Text, want.Text)
		}
		if back.ID != want.ID {
			t.Fatalf("report stored under %s calls itself %s", want.ID, back.ID)
		}
	}
}

// Acceptance clause 3: two identical-text reports in the same one-second
// bucket are both retrievable afterwards by distinct ids.
//
// This is the clause that overrides the idempotency NewID's old comment
// claimed ("the same report saved twice in one second is one record rather
// than two"). A content digest cannot tell one report stored twice from two
// identical reports, and it used to resolve that ambiguity by deleting one.
// De-duplicating a redelivery belongs at the redelivery site, keyed on an
// explicit id — which is how 🎯T731 already does it (🎯T747).
func TestT746IdenticalTextInOneSecondGetsDistinctIDs(t *testing.T) {
	const agent = "jv-t746-identical"
	const ack = "No response requested."
	state := t.TempDir()

	first, err := Save(state, agent, ack, t746At)
	if err != nil {
		t.Fatalf("save first: %v", err)
	}
	second, err := Save(state, agent, ack, t746At)
	if err != nil {
		t.Fatalf("save second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("both acks got id %s — the second overwrote the first", first.ID)
	}
	if first.ID != "20260920T171704Z-85caba2f" {
		t.Fatalf("first ack id = %s, want the unchanged base id 20260920T171704Z-85caba2f", first.ID)
	}
	for _, id := range []string{first.ID, second.ID} {
		back, err := Load(state, agent, id)
		if err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		if back.Text != ack {
			t.Fatalf("report %s reads back %q, want %q", id, back.Text, ack)
		}
	}
	recs, err := List(state, agent)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("List returned %d reports, want 2", len(recs))
	}
	// Lexical order is still chronological: a sibling sorts immediately after
	// the base it extends, so same-second siblings read in arrival order and
	// Latest still answers with the newest.
	if recs[0].ID != first.ID || recs[1].ID != second.ID {
		t.Fatalf("List order = [%s %s], want arrival order [%s %s]",
			recs[0].ID, recs[1].ID, first.ID, second.ID)
	}
	latest, err := Latest(state, agent)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.ID != second.ID {
		t.Fatalf("Latest = %s, want the last one stored %s", latest.ID, second.ID)
	}
}

// Acceptance clause 2: ids are unique per stored report. Twenty saves of one
// text inside one second are twenty records, not one.
func TestT746EveryStoredReportKeepsItsOwnID(t *testing.T) {
	const agent = "jv-t746-unique"
	const n = 20
	state := t.TempDir()
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		rec, err := Save(state, agent, "identical body", t746At)
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if seen[rec.ID] {
			t.Fatalf("save %d reused id %s", i, rec.ID)
		}
		seen[rec.ID] = true
	}
	recs, err := List(state, agent)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != n {
		t.Fatalf("stored %d reports, List returned %d", n, len(recs))
	}
}

// The same guarantee under a real race, since two saves for one report can
// arrive from the notify path and the 🎯T392.7 reroute at once. This is also
// the oracle for the temp file: path+".tmp" was shared by every save of one
// id, so racing saves could interleave their bytes into it.
func TestT746ConcurrentSavesOfOneTextAllSurvive(t *testing.T) {
	const agent = "jv-t746-race"
	const n = 16
	state := t.TempDir()
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, err := Save(state, agent, "concurrent body", t746At)
			ids[i], errs[i] = rec.ID, err
		}(i)
	}
	wg.Wait()
	seen := make(map[string]bool, n)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent save %d: %v", i, err)
		}
		if seen[ids[i]] {
			t.Fatalf("concurrent save %d reused id %s", i, ids[i])
		}
		seen[ids[i]] = true
	}
	recs, err := List(state, agent)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != n {
		t.Fatalf("%d concurrent saves produced %d records", n, len(recs))
	}
	for _, rec := range recs {
		back, err := Load(state, agent, rec.ID)
		if err != nil {
			t.Fatalf("load %s: %v", rec.ID, err)
		}
		if back.Text != "concurrent body" {
			t.Fatalf("report %s reads back %q — a racing write was torn", rec.ID, back.Text)
		}
	}
}

// The mutation, run against the product path on one input: restoring the
// rename loses a report, keeping the claim does not.
func TestT746MutationRestoringTheOverwriteLosesAReport(t *testing.T) {
	const agent = "jv-t746-mutation"
	const text = "No response requested."
	id := NewID(t746At, text)
	rec := Record{ID: id, Agent: agent, At: t746At, Text: text, Bytes: len(text)}

	mutated := t746AgentDir(t, t.TempDir(), agent)
	saveOverwriting(t, mutated, rec)
	saveOverwriting(t, mutated, rec)
	if got := t746CountJSON(t, mutated); got != 1 {
		t.Fatalf("mutation: rename path kept %d of 2 reports, want 1 — this test no longer demonstrates the defect", got)
	}

	state := t.TempDir()
	if _, err := Save(state, agent, text, t746At); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if _, err := Save(state, agent, text, t746At); err != nil {
		t.Fatalf("save second: %v", err)
	}
	dir, err := AgentDir(state, agent)
	if err != nil {
		t.Fatalf("AgentDir: %v", err)
	}
	if got := t746CountJSON(t, dir); got != 2 {
		t.Fatalf("product path kept %d of 2 reports — the overwrite is back", got)
	}
}

// 🎯T388, restated against this store so the two guarantees cannot drift: a
// stored report is retrievable by id after its author is gone. Nothing here
// consults a registry, which is the point — Load and Latest answer for an
// agent the fleet has already deregistered.
func TestT746StoredReportOutlivesItsAuthor(t *testing.T) {
	const agent = "jv-t746-durable"
	const body = "## Asks\n\n- Do not let the owner rule on EC-6 using my earlier premise.\n"
	state := t.TempDir()
	rec, err := Save(state, agent, body, t746At)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// The author is gone; only the store remains.
	back, err := Load(state, agent, rec.ID)
	if err != nil {
		t.Fatalf("load after deregistration: %v", err)
	}
	if back.Text != body {
		t.Fatalf("stored report reads back %q, want %q", back.Text, body)
	}
	latest, err := Latest(state, agent)
	if err != nil {
		t.Fatalf("latest after deregistration: %v", err)
	}
	if latest.Text != body {
		t.Fatalf("Latest reads back %q, want %q", latest.Text, body)
	}
	if latest.Handle().ReportID != rec.ID {
		t.Fatalf("handle names %s, want %s", latest.Handle().ReportID, rec.ID)
	}
}

// Back-compat, which is the reason the base id was left alone: an id minted by
// the old scheme still loads, and a save that collides with nothing mints
// exactly what NewID proposes — byte-identical to what the old path wrote.
// Ids appear in truncation markers, in jevons_agent_report_read arguments, in
// 🎯T731's parent-copy report_id and in hand-written attestations, and nothing
// in the tree parses one: every use is %s formatting, map-key equality or
// filepath.Join, with no time.Parse of the layout anywhere. Changing the base
// id would have been safe and pointless; leaving it alone costs nothing.
func TestT746BaseIDIsUnchangedAndOldIDsStillLoad(t *testing.T) {
	const agent = "jv-t746-compat"
	const text = "a report nobody else wrote"
	state := t.TempDir()

	rec, err := Save(state, agent, text, t746At)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if want := NewID(t746At, text); rec.ID != want {
		t.Fatalf("non-colliding save minted %s, want the unchanged base id %s", rec.ID, want)
	}

	// An id written before this change, by the path this change replaced.
	old := Record{
		ID:    "20260809T101500Z-deadbeef",
		Agent: agent,
		At:    time.Date(2026, 8, 9, 10, 15, 0, 0, time.UTC),
		Text:  "stored under the pre-🎯T746 scheme",
	}
	old.Bytes = len(old.Text)
	dir := t746AgentDir(t, state, agent)
	saveOverwriting(t, dir, old)
	back, err := Load(state, agent, old.ID)
	if err != nil {
		t.Fatalf("an id cited before this change no longer loads: %v", err)
	}
	if back.Text != old.Text {
		t.Fatalf("old report reads back %q, want %q", back.Text, old.Text)
	}
	if _, err := fmt.Sscanf(back.ID, "%s", new(string)); err != nil {
		t.Fatalf("old id is not even a plain string: %v", err)
	}
}
