// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T441. The defect these pin arrived through the tool 🎯T386 built to stop
// it: with no subcommand allowlist, anything that was not last/show/check was
// executed, so `gate ls` ran ls and filed `GATE ls exit=0 GREEN id=51a81f02`
// in the store that `gate last` reads back and a finish report cites. A typo
// minted a citable green that attested nothing.
//
// The tests come in two halves because the defect has two: the writer that
// mints such records, and the records it already minted.

// countRecords is how many runs the citable store would hand to a reader.
// Voided records live in a subdirectory and are deliberately not counted.
func countRecords(t *testing.T, store string) int {
	t.Helper()
	entries, err := os.ReadDir(store)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading store: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// Acceptance 1 and 3: a word that is not a subcommand is a usage error that
// records nothing, and the separated form still works.
func TestT441MistypedSubcommandIsAnErrorNotAGate(t *testing.T) {
	store := t.TempDir()

	// The two that actually happened, plus the reproduction from the ledger.
	// `ls` is the dangerous one — it exists everywhere and exits 0, so the
	// record it used to mint was a GREEN under a plausible-looking name.
	for _, word := range []string{"ls", "list", "zzz-definitely-not-a-subcommand"} {
		before := countRecords(t, store)
		code, out := runGate(t, store, word)
		if code == 0 {
			t.Fatalf("gate %s exited 0; a mistyped subcommand must fail\n%s", word, out)
		}
		if !strings.Contains(out, word) {
			t.Fatalf("gate %s did not name the unknown subcommand:\n%s", word, out)
		}
		if !strings.Contains(out, "not a gate subcommand") {
			t.Fatalf("gate %s did not say what was wrong:\n%s", word, out)
		}
		if after := countRecords(t, store); after != before {
			t.Fatalf("gate %s wrote %d record(s); a run that never happened must "+
				"leave no evidence behind\n%s", word, after-before, out)
		}
		// The whole failure mode is the store answering a later reader, so
		// check the surface a worker actually cites rather than the file count
		// alone.
		if _, last := runGate(t, store, "last"); strings.Contains(last, "GATE "+word+" ") {
			t.Fatalf("gate last hands back a record for the typo %q:\n%s", word, last)
		}
	}

	// And the separated form is untouched: this is a real gate and records one.
	code, out := runGate(t, store, "--", "true")
	if code != 0 {
		t.Fatalf("gate -- true exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("gate -- true did not attest green:\n%s", out)
	}
	if n := countRecords(t, store); n != 1 {
		t.Fatalf("store holds %d records after one real gate, want 1", n)
	}
}

// The separator is what disambiguates, so it has to win in both directions: a
// program whose name collides with a subcommand still runs when asked for
// explicitly. Without this, closing the hole would have shadowed real gates.
func TestT441SeparatorStillRunsASubcommandNamedProgram(t *testing.T) {
	store := t.TempDir()

	bin := filepath.Join(t.TempDir(), "last")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ran the program\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := runGate(t, store, "--", bin)
	if code != 0 {
		t.Fatalf("gate -- <a program called last> exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "ran the program") {
		t.Fatalf("the explicit form did not run the program:\n%s", out)
	}
}

// Acceptance 2's other edge: the bare fallthrough is gone, so a command typed
// without the separator is refused rather than silently run. Refusing a real
// command is a nuisance; running a typo is a false green, and the message
// names the fix.
func TestT441BareCommandIsRefusedWithTheFixNamed(t *testing.T) {
	store := t.TempDir()

	code, out := runGate(t, store, "make", "test-go")
	if code == 0 {
		t.Fatalf("gate ran a bare command; the separator must be required\n%s", out)
	}
	if !strings.Contains(out, "gate -- make test-go") {
		t.Fatalf("the refusal did not show the separated form to use:\n%s", out)
	}
	if n := countRecords(t, store); n != 0 {
		t.Fatalf("a refused invocation wrote %d record(s)", n)
	}
}

// The second half: the records the old writer already minted. seedLegacyRecord
// writes one the way that writer did — no Explicit marker, because it had no
// notion of one.
func seedLegacyRecord(t *testing.T, store *gate.Store, id, word string, status int, verdict gate.Verdict) {
	t.Helper()
	rec := &gate.Record{
		ID:          id,
		Name:        word,
		Command:     []string{word},
		Started:     time.Date(2026, 8, 10, 20, 52, 0, 0, time.UTC),
		Ended:       time.Date(2026, 8, 10, 20, 52, 1, 0, time.UTC),
		ExitStatus:  status,
		StatusKnown: true,
		Verdict:     verdict,
	}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(id), []byte("whatever "+word+" printed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The predicate is the deliverable, not the four ids somebody happened to
// find: the oldest of those was five days older than the session that noticed,
// so the store had been accumulating them unobserved.
func TestT441SweepPredicateSeparatesTyposFromGates(t *testing.T) {
	dir := t.TempDir()
	store, err := gate.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The real strays, by shape.
	seedLegacyRecord(t, store, "51a81f02", "ls", 0, gate.VerdictGreen)
	seedLegacyRecord(t, store, "2f1ddd91", "list", 2, gate.VerdictRed)
	seedLegacyRecord(t, store, "2432761f", "list", 2, gate.VerdictRed)

	// A real gate of every shape the daily store actually contains.
	for i, argv := range [][]string{
		{"make", "test-go"},
		{"go", "test", "./..."},
		{"node", "scripts/chat-ui-test/t370-fleet-cycle-test.js"},
		{"cargo", "test"},
		{"sh", "-c", "go vet ./... && go test -race -count=1 ./..."},
	} {
		rec := &gate.Record{
			ID: "real" + string(rune('a'+i)), Name: "real", Command: argv,
			StatusKnown: true, Verdict: gate.VerdictGreen,
			Started: time.Date(2026, 8, 12, 0, 0, i, 0, time.UTC),
		}
		if err := store.Save(rec); err != nil {
			t.Fatal(err)
		}
	}

	// And the case the Explicit marker exists for: a deliberate single-word
	// gate, run through the fixed binary. Bare-word shaped, and not a typo.
	if err := store.Save(&gate.Record{
		ID: "deliberate", Name: "pytest", Command: []string{"pytest"},
		Explicit: true, StatusKnown: true, Verdict: gate.VerdictGreen,
		Started: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.Sweep(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range results {
		got[r.Record.ID] = true
		if r.Voided {
			t.Fatalf("a dry sweep voided %s", r.Record.ID)
		}
		if r.Reason == "" {
			t.Fatalf("%s was swept with no reason given", r.Record.ID)
		}
	}
	want := map[string]bool{"51a81f02": true, "2f1ddd91": true, "2432761f": true}
	for id := range want {
		if !got[id] {
			t.Errorf("sweep missed the stray record %s", id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("sweep would void %s, which is a real gate", id)
		}
	}
	if n := countRecords(t, dir); n != 9 {
		t.Fatalf("a dry sweep changed the store: %d records, want 9", n)
	}
}

// Voiding is quarantine, not deletion, and the difference has to be visible
// on the two surfaces that matter: what a reader is handed by default, and
// what a report citing the record gets told.
func TestT441VoidedRecordIsUncitableButStillReadable(t *testing.T) {
	dir := t.TempDir()
	store, err := gate.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyRecord(t, store, "51a81f02", "ls", 0, gate.VerdictGreen)

	// Before: this is the false green the target is about. A report citing it
	// passes the check, because the store vouches for it.
	report := "🎯T441 done, tests pass.\n\n" +
		"    GATE ls exit=0 GREEN id=51a81f02 out=abc123def456 dur=0.1s\n"
	if flags := gate.FlagFalseGreen(report, store.Lookup); len(flags) != 0 {
		t.Fatalf("the pre-sweep store already refused the citation: %v", flags)
	}

	results, err := store.Sweep(true, func() time.Time {
		return time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Voided || results[0].Err != nil {
		t.Fatalf("sweep -void did not void the record: %+v", results)
	}

	// Gone from the citable store, so `gate last` cannot hand it to anyone.
	if n := countRecords(t, dir); n != 0 {
		t.Fatalf("%d record(s) still citable after the sweep", n)
	}
	if recs, err := store.Recent(0); err != nil || len(recs) != 0 {
		t.Fatalf("Recent still returns the voided record: %v %v", recs, err)
	}

	// Still readable by id, with the reason — the point of quarantining.
	rec, ok, err := store.LoadVoided("51a81f02")
	if err != nil || !ok {
		t.Fatalf("voided record is not readable by id: %v %v", ok, err)
	}
	if rec.Verdict != gate.VerdictVoid || rec.VoidReason == "" || rec.VoidedAt.IsZero() {
		t.Fatalf("voided record does not describe itself: %+v", rec)
	}
	logPath := filepath.Join(dir, gate.VoidedDir, "51a81f02.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("the run's output did not move with its record: %v", err)
	}
	// The output is the only remaining way to see what the mistyped run did,
	// so the record has to point at where it now is rather than where it was.
	if rec.OutputPath != logPath {
		t.Fatalf("voided record points at %q, but its log is at %q",
			rec.OutputPath, logPath)
	}

	// And the report-time half, which is the requirement a writer-only fix
	// would have missed: `check` still resolves the id, so a report citing it
	// is flagged for what is wrong rather than passing unnoticed.
	flags := gate.FlagFalseGreen(report, store.Lookup)
	if len(flags) == 0 {
		t.Fatal("a report citing a voided record passes the false-green check")
	}
	banner := gate.Banner(flags)
	if !strings.Contains(banner, "51a81f02") || !strings.Contains(banner, string(gate.VerdictVoid)) {
		t.Fatalf("the banner does not say the cited record was voided:\n%s", banner)
	}
}

// A record that attests nothing must not be reachable as evidence through the
// CLI either, and the CLI is what workers and the daemon actually run.
func TestT441SweepThroughTheCLI(t *testing.T) {
	dir := t.TempDir()
	store, err := gate.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyRecord(t, store, "51a81f02", "ls", 0, gate.VerdictGreen)

	// Dry first: it names the candidate and exits non-zero so a caller cannot
	// mistake "there is junk here" for "the store is clean", and it changes
	// nothing.
	code, out := runGate(t, dir, "sweep")
	if code == 0 {
		t.Fatalf("a dry sweep with a candidate exited 0:\n%s", out)
	}
	if !strings.Contains(out, "51a81f02") {
		t.Fatalf("dry sweep did not name the candidate:\n%s", out)
	}
	if n := countRecords(t, dir); n != 1 {
		t.Fatalf("dry sweep changed the store")
	}

	if code, out = runGate(t, dir, "sweep", "-void"); code != 0 {
		t.Fatalf("sweep -void exited %d:\n%s", code, out)
	}
	if n := countRecords(t, dir); n != 0 {
		t.Fatalf("sweep -void left %d citable record(s)", n)
	}

	// `gate last` is the surface the false green travelled on. It must now
	// have nothing to say.
	if code, out = runGate(t, dir, "last"); code == 0 || strings.Contains(out, "GATE ls ") {
		t.Fatalf("gate last still offers the voided record:\n%s", out)
	}

	// `gate show` still answers, because a record that vanished without a
	// stated reason is indistinguishable from evidence being tidied away.
	code, out = runGate(t, dir, "show", "51a81f02")
	if code == 0 {
		t.Fatalf("gate show reported a voided record as a pass:\n%s", out)
	}
	if !strings.Contains(out, string(gate.VerdictVoid)) || !strings.Contains(out, "voided:") {
		t.Fatalf("gate show did not explain the voided record:\n%s", out)
	}

	// Sweeping again is a no-op rather than an error.
	if code, out = runGate(t, dir, "sweep"); code != 0 {
		t.Fatalf("a second sweep on a clean store exited %d:\n%s", code, out)
	}
}
