// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 🎯T749 — two delivery records written in the same second do not overwrite
// each other.
//
// newID is timestamp(1s) + sha256(agent+"\n"+needle)[:4], and that digest is
// derived entirely from the lookup key: every confirmation of the same message
// to the same agent proposes the same suffix. Record wrote <id>.json with a
// rename, and rename overwrites, so two such records inside one second
// resolved to one file and the later one deleted the earlier.
//
// This is the same construction and the same defect 🎯T746 fixed in
// internal/agentreport, carried here so the two stores cannot drift. The
// shape is commoner in this store than in that one: the send path records a
// "begun" outcome and then the outcome it actually got, for the same agent and
// the same needle.
//
// THE 🎯T417 GUARANTEE THIS SERVES, stated here so the two cannot drift: a
// delivery that was confirmed stays provable afterwards, from this store,
// after the receiving agent's transcript has been compacted away.
// TestT749ConfirmationOutlivesTheTranscript below asserts that guarantee
// against the same store this target changes. A store that silently drops a
// record does not make the guarantee, whatever its retrieval path does — the
// dropped record may be the one carrying the outcome, the session id, or the
// operator-facing detail.

// t749At is one fixed second. Every record in these tests is stamped with it,
// so the one-second bucket is not a matter of test timing luck.
var t749At = time.Date(2026, 9, 21, 15, 11, 53, 0, time.UTC)

// t749Needle is long enough to form a real needle; Record refuses short ones.
const t749Needle = "endorsement: T749 delivery-store collision; please continue."

// recordOverwriting is the pre-🎯T749 store path, kept in its original shape:
// write a temp file beside the target and rename over whatever is there.
//
// It is here so the mutation is demonstrated rather than asserted. The tests
// below run it and the product path over identical input and show this one
// losing a record — so if Record is ever reverted to this behaviour, the
// assertions in this file stop passing for a reason the file itself explains.
func recordOverwriting(t *testing.T, dir string, rec Evidence) {
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

func t749CountJSON(t *testing.T, dir string) int {
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

func t749AgentDir(t *testing.T, state, agent string) string {
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

// t749ReadAll loads every record in an agent's directory, keyed by the id the
// file is named for, and checks each file's stored id agrees with its name —
// a body published under a name that is not its own id is a record that cannot
// be cited.
func t749ReadAll(t *testing.T, dir string) map[string]Evidence {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	out := map[string]Evidence{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		rec, err := load(dir, id)
		if err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		if rec.ID != id {
			t.Fatalf("file %s.json stores id %q — the body was published under a name that is not its id", id, rec.ID)
		}
		out[id] = rec
	}
	return out
}

// Acceptance clause 1: two distinct records minted in the same one-second
// bucket are both retrievable afterwards, by distinct ids.
//
// The product path is driven through Record, with the legacy path in the same
// test as the control: same input, one file, first record gone.
func TestT749TwoRecordsInOneSecondGetDistinctIDs(t *testing.T) {
	const agent = "jv-t749-same-second"

	begun := Evidence{Agent: agent, SessionID: "first-session", Needle: t749Needle,
		Detail: "transcript gained a user message carrying this payload", Outcome: "begun"}
	confirmed := Evidence{Agent: agent, SessionID: "second-session", Needle: t749Needle,
		Detail: "turn ended with a response", Outcome: "confirmed"}

	// Control: the legacy path, on the ids the product path would propose.
	legacyDir := t749AgentDir(t, t.TempDir(), agent)
	legacyID := newID(t749At, agent, t749Needle)
	a, b := begun, confirmed
	a.ID, b.ID = legacyID, legacyID
	a.At, b.At = t749At, t749At
	recordOverwriting(t, legacyDir, a)
	recordOverwriting(t, legacyDir, b)
	if n := t749CountJSON(t, legacyDir); n != 1 {
		t.Fatalf("control: legacy path wrote %d files, want 1 — the mutation is not the mutation", n)
	}

	state := t.TempDir()
	first, err := Record(state, begun, t749At)
	if err != nil {
		t.Fatalf("Record begun: %v", err)
	}
	second, err := Record(state, confirmed, t749At)
	if err != nil {
		t.Fatalf("Record confirmed: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("both records got id %q — the second overwrote the first", first.ID)
	}
	if first.ID != legacyID {
		t.Fatalf("first record got id %q, want the unchanged base id %q — ids already on disk must still resolve", first.ID, legacyID)
	}

	dir := t749AgentDir(t, state, agent)
	if n := t749CountJSON(t, dir); n != 2 {
		t.Fatalf("product path kept %d of 2 records — the overwrite is back", n)
	}
	got := t749ReadAll(t, dir)
	gotFirst, ok := got[first.ID]
	if !ok {
		t.Fatalf("id %q is not retrievable", first.ID)
	}
	gotSecond, ok := got[second.ID]
	if !ok {
		t.Fatalf("id %q is not retrievable", second.ID)
	}
	if gotFirst.Outcome != "begun" || gotFirst.SessionID != "first-session" {
		t.Fatalf("first record came back as %+v", gotFirst)
	}
	if gotSecond.Outcome != "confirmed" || gotSecond.SessionID != "second-session" {
		t.Fatalf("second record came back as %+v", gotSecond)
	}

	// Lookup walks ids in reverse lexical order and must still answer with
	// the newest confirmation, not the sibling's arrival order reversed.
	latest, found, err := Lookup(state, agent, t749Needle)
	if err != nil || !found {
		t.Fatalf("Lookup found=%v err=%v", found, err)
	}
	if latest.ID != second.ID || latest.Outcome != "confirmed" {
		t.Fatalf("Lookup returned %+v, want the newest record %q/confirmed", latest, second.ID)
	}
}

// The mutation goes RED: restore the overwrite and a record is lost.
//
// This runs the legacy writer over the product path's own proposed ids and
// asserts the loss, so the assertion above is anchored to a demonstrated
// failure rather than to a number someone chose.
func TestT749MutationRestoringTheOverwriteLosesARecord(t *testing.T) {
	const agent = "jv-t749-mutation"
	dir := t749AgentDir(t, t.TempDir(), agent)
	id := newID(t749At, agent, t749Needle)

	for _, outcome := range []string{"begun", "confirmed"} {
		recordOverwriting(t, dir, Evidence{ID: id, Agent: agent, Needle: t749Needle,
			At: t749At, Outcome: outcome})
	}
	if n := t749CountJSON(t, dir); n != 1 {
		t.Fatalf("legacy path wrote %d files, want 1", n)
	}
	survivor, err := load(dir, id)
	if err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	if survivor.Outcome != "confirmed" {
		t.Fatalf("survivor outcome=%q", survivor.Outcome)
	}
	// The "begun" record is gone, with nothing naming it — which is the
	// silence this target ends.
	if _, err := load(dir, id+"-2"); err == nil {
		t.Fatal("legacy path somehow produced a sibling — the control is not the control")
	}
}

// Every record in a burst keeps its own id, including a burst larger than any
// digest suffix could disambiguate.
func TestT749EveryRecordInOneSecondKeepsItsOwnID(t *testing.T) {
	const agent = "jv-t749-burst"
	const n = 20
	state := t.TempDir()

	ids := map[string]string{}
	for i := 0; i < n; i++ {
		rec, err := Record(state, Evidence{Agent: agent, Needle: t749Needle,
			Detail: fmt.Sprintf("attempt %d", i), Outcome: "begun"}, t749At)
		if err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
		if prev, dup := ids[rec.ID]; dup {
			t.Fatalf("id %q reused by %s and attempt %d", rec.ID, prev, i)
		}
		ids[rec.ID] = fmt.Sprintf("attempt %d", i)
	}

	dir := t749AgentDir(t, state, agent)
	if got := t749CountJSON(t, dir); got != n {
		t.Fatalf("%d of %d records survived one second", got, n)
	}
	got := t749ReadAll(t, dir)
	for id, want := range ids {
		rec, ok := got[id]
		if !ok {
			t.Fatalf("id %q is not retrievable", id)
		}
		if rec.Detail != want {
			t.Fatalf("id %q holds detail %q, want %q", id, rec.Detail, want)
		}
	}
}

// Concurrent records racing for one id all survive, and none is left holding
// another's bytes — the temp-file half of the same defect.
func TestT749ConcurrentRecordsAllSurvive(t *testing.T) {
	const agent = "jv-t749-concurrent"
	const n = 16
	state := t.TempDir()
	if _, err := AgentDir(state, agent); err != nil {
		t.Fatalf("AgentDir: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]Evidence, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = Record(state, Evidence{Agent: agent, Needle: t749Needle,
				Detail: fmt.Sprintf("racer %02d", i), Outcome: "begun"}, t749At)
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if seen[results[i].ID] {
			t.Fatalf("racer %d got duplicate id %q", i, results[i].ID)
		}
		seen[results[i].ID] = true
	}

	dir := t749AgentDir(t, state, agent)
	if got := t749CountJSON(t, dir); got != n {
		t.Fatalf("%d of %d concurrent records survived", got, n)
	}
	got := t749ReadAll(t, dir)
	details := map[string]bool{}
	for id, rec := range got {
		if !seen[id] {
			t.Fatalf("stored id %q was returned to nobody", id)
		}
		if details[rec.Detail] {
			t.Fatalf("detail %q appears in two files — temp files interleaved", rec.Detail)
		}
		details[rec.Detail] = true
	}
	if len(details) != n {
		t.Fatalf("%d distinct details across %d files", len(details), n)
	}
	// No temp file left behind to be mistaken for a record.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file %s survived the write", e.Name())
		}
	}
}

// Back-compat: a record that does not collide mints exactly the id the old
// scheme minted, and an id written by the old scheme still loads.
func TestT749BaseIDIsUnchangedAndOldIDsStillLoad(t *testing.T) {
	const agent = "jv-t749-compat"
	state := t.TempDir()

	want := newID(t749At, agent, t749Needle)
	rec, err := Record(state, Evidence{Agent: agent, Needle: t749Needle, Outcome: "begun"}, t749At)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.ID != want {
		t.Fatalf("non-colliding record got id %q, want %q", rec.ID, want)
	}

	// A file written the old way, under a different second, still reads back
	// and still answers Lookup.
	older := t749At.Add(-time.Hour)
	legacyID := newID(older, agent, t749Needle)
	dir := t749AgentDir(t, state, agent)
	recordOverwriting(t, dir, Evidence{ID: legacyID, Agent: agent, Needle: t749Needle,
		At: older, Outcome: "confirmed", Detail: "written by the pre-T749 path"})
	if _, err := load(dir, legacyID); err != nil {
		t.Fatalf("legacy id %q no longer loads: %v", legacyID, err)
	}
	latest, found, err := Lookup(state, agent, t749Needle)
	if err != nil || !found {
		t.Fatalf("Lookup found=%v err=%v", found, err)
	}
	if latest.ID != rec.ID {
		t.Fatalf("Lookup returned %q, want the newer record %q", latest.ID, rec.ID)
	}
}

// The 🎯T417 guarantee, restated against the store this target changes: a
// confirmation stays provable after the receiving transcript is compacted
// away, and the record that proves it is the one that was written — not
// whichever same-second sibling happened to win a rename.
func TestT749ConfirmationOutlivesTheTranscript(t *testing.T) {
	const agent = "jv-t749-outlives"
	state := t.TempDir()
	payload := strings.Repeat("a payload long enough to form a distinctive needle ", 3)

	begun, err := RecordPayload(state, agent, "sess-1", payload, "send turn began", "begun", t749At)
	if err != nil {
		t.Fatalf("RecordPayload begun: %v", err)
	}
	confirmed, err := RecordPayload(state, agent, "sess-1", payload, "turn ended with a response", "confirmed", t749At)
	if err != nil {
		t.Fatalf("RecordPayload confirmed: %v", err)
	}
	if begun.ID == confirmed.ID {
		t.Fatalf("both confirmations got id %q — one of them is gone", begun.ID)
	}

	// Compaction-shaped wipe of the receiving transcript.
	transcript := filepath.Join(state, "sessions", "sess-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(transcript, []byte(`{"type":"user","message":{"content":"handover only"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	ok, err := WasDelivered(state, agent, payload)
	if err != nil {
		t.Fatalf("WasDelivered: %v", err)
	}
	if !ok {
		t.Fatal("confirmed delivery became unprovable after transcript wipe")
	}
	dir := t749AgentDir(t, state, agent)
	got := t749ReadAll(t, dir)
	if len(got) != 2 {
		t.Fatalf("%d of 2 confirmations outlived the transcript", len(got))
	}
	outcomes := map[string]string{}
	for id, rec := range got {
		outcomes[rec.Outcome] = id
	}
	if outcomes["begun"] == "" || outcomes["confirmed"] == "" {
		t.Fatalf("outcomes on disk: %v — the record carrying an outcome was lost", outcomes)
	}
}
