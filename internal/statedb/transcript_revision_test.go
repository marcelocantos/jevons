// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package statedb

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestTranscriptRevisionSurvivesEmptyReplacementAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := s.Upsert("owner", []Event{{Index: 1, ID: "e:1", Body: fmt.Sprint(i)}}); err != nil {
			t.Fatal(err)
		}
		snapshot, err := s.Snapshot("owner")
		if err != nil || snapshot.Revision != int64(i) || len(snapshot.Events) != 1 || snapshot.Events[0].Body != fmt.Sprint(i) {
			t.Fatalf("snapshot=%+v err=%v", snapshot, err)
		}
	}
	if err := s.ReplaceAll("owner", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snapshot, err := s.Snapshot("owner")
	if err != nil || snapshot.Revision != 3 || len(snapshot.Events) != 0 {
		t.Fatalf("reopened snapshot=%+v err=%v", snapshot, err)
	}
	if ok, err := s.ShouldImport("owner"); err != nil || ok {
		t.Fatalf("empty canonical history became importable: %v %v", ok, err)
	}
	if ok, err := s.ShouldImport("other"); err != nil || !ok {
		t.Fatalf("unrelated agent was initialized: %v %v", ok, err)
	}
}

func TestLegacyTranscriptInitializationWithoutRevision(t *testing.T) {
	s := testStore(t)
	if _, err := s.db.Exec(`INSERT INTO transcript_events (agent, idx, id, body) VALUES ('rows', 1, 'e:1', 'old')`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWatermark("empty", "/old.jsonl", 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"rows", "empty"} {
		if ok, err := s.ShouldImport(agent); err != nil || ok {
			t.Fatalf("legacy %s became importable: %v %v", agent, ok, err)
		}
		snapshot, err := s.Snapshot(agent)
		if err != nil || snapshot.Revision != 0 {
			t.Fatalf("legacy %s snapshot=%+v err=%v", agent, snapshot, err)
		}
		if err := s.Upsert(agent, []Event{{Index: 1, Body: "new"}}); err != nil {
			t.Fatal(err)
		}
		snapshot, err = s.Snapshot(agent)
		if err != nil || snapshot.Revision != 1 || len(snapshot.Events) != 1 || snapshot.Events[0].Body != "new" {
			t.Fatalf("updated legacy %s snapshot=%+v err=%v", agent, snapshot, err)
		}
	}
}

func TestImportTranscriptCannotOverwriteWriteAfterInitialCheck(t *testing.T) {
	s := testStore(t)
	if ok, err := s.ShouldImport("worker"); err != nil || !ok {
		t.Fatalf("initial check=%v %v", ok, err)
	}
	// The caller has read the legacy file. A live send wins before it commits.
	legacy := []Event{{Index: 1, Body: "obsolete"}}
	if err := s.Upsert("worker", []Event{{Index: 1, Body: "live owner request"}}); err != nil {
		t.Fatal(err)
	}
	if imported, err := s.ImportTranscript("worker", "/old.jsonl", 8, legacy); err != nil || imported {
		t.Fatalf("late import=%v %v", imported, err)
	}
	snapshot, err := s.Snapshot("worker")
	if err != nil || snapshot.Revision != 1 || len(snapshot.Events) != 1 || snapshot.Events[0].Body != "live owner request" {
		t.Fatalf("live write lost: %+v %v", snapshot, err)
	}
	if watermark, err := s.GetWatermark("worker"); err != nil || watermark != nil {
		t.Fatalf("declined import wrote watermark: %+v %v", watermark, err)
	}
}

func TestImportTranscriptCommitsAllStateOrNone(t *testing.T) {
	for _, phase := range []string{"rows", "watermark", "revision"} {
		t.Run(phase, func(t *testing.T) {
			s := testStore(t)
			var trigger string
			switch phase {
			case "rows":
				trigger = `CREATE TRIGGER reject_import BEFORE INSERT ON transcript_events BEGIN SELECT RAISE(ABORT, 'row failure'); END`
			case "watermark":
				trigger = `CREATE TRIGGER reject_import BEFORE INSERT ON import_watermark BEGIN SELECT RAISE(ABORT, 'watermark failure'); END`
			case "revision":
				trigger = `CREATE TRIGGER reject_import BEFORE INSERT ON schema_meta BEGIN SELECT RAISE(ABORT, 'revision failure'); END`
			}
			if _, err := s.db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			if imported, err := s.ImportTranscript("worker", "/old.jsonl", 42, []Event{{Index: 1, Body: "old"}}); err == nil || imported {
				t.Fatalf("injected failure accepted: %v %v", imported, err)
			}
			snapshot, err := s.Snapshot("worker")
			if err != nil || snapshot.Revision != 0 || len(snapshot.Events) != 0 {
				t.Fatalf("partial import: %+v %v", snapshot, err)
			}
			if watermark, err := s.GetWatermark("worker"); err != nil || watermark != nil {
				t.Fatalf("partial watermark: %+v %v", watermark, err)
			}
			if ok, err := s.ShouldImport("worker"); err != nil || !ok {
				t.Fatalf("failed import blocked retry: %v %v", ok, err)
			}
			if _, err := s.db.Exec(`DROP TRIGGER reject_import`); err != nil {
				t.Fatal(err)
			}
			if imported, err := s.ImportTranscript("worker", "/old.jsonl", 42, nil); err != nil || !imported {
				t.Fatalf("empty retry failed: %v %v", imported, err)
			}
			if ok, err := s.ShouldImport("worker"); err != nil || ok {
				t.Fatalf("completed empty import became importable: %v %v", ok, err)
			}
		})
	}
}

func TestInvalidRevisionOrRowsRollBackReplacement(t *testing.T) {
	for _, value := range []string{"oops", "-1", "0", "9223372036854775808", "9223372036854775807"} {
		t.Run(value, func(t *testing.T) {
			s := testStore(t)
			if err := s.Upsert("worker", []Event{{Index: 1, Body: "keep"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE schema_meta SET value = ? WHERE key = ?`, value, transcriptRevisionPrefix+"worker"); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceAll("worker", nil); err == nil {
				t.Fatal("invalid/exhausted revision accepted")
			}
			rows, err := s.Range("worker", 1, 2)
			if err != nil || len(rows) != 1 || rows[0].Body != "keep" {
				t.Fatalf("failed replacement removed rows: %+v %v", rows, err)
			}
		})
	}
	s := testStore(t)
	if err := s.Upsert("worker", []Event{{Index: 0, Body: "invalid"}}); err == nil {
		t.Fatal("invalid row accepted")
	}
	if ok, err := s.ShouldImport("worker"); err != nil || !ok {
		t.Fatalf("invalid upsert initialized history: %v %v", ok, err)
	}
}

func TestConcurrentTranscriptImportsHaveOneWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	stores := make([]*Store, 2)
	for i := range stores {
		var err error
		stores[i], err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer stores[i].Close()
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	won := make([]bool, 2)
	errs := make([]error, 2)
	for i, store := range stores {
		wg.Go(func() {
			<-start
			won[i], errs[i] = store.ImportTranscript("worker", "/old.jsonl", 1, []Event{{Index: 1, Body: fmt.Sprint(i)}})
		})
	}
	close(start)
	wg.Wait()
	winners := 0
	for i, didImport := range won {
		if didImport {
			winners++
		}
		if errs[i] != nil {
			// A deferred SQLite writer may lose with BUSY; retry must decline.
			if imported, err := stores[i].ImportTranscript("worker", "/old.jsonl", 1, nil); err != nil || imported {
				t.Fatalf("loser retry=%v %v; initial=%v", imported, err, errs[i])
			}
		}
	}
	if winners != 1 {
		t.Fatalf("winners=%v errors=%v", won, errs)
	}
	snapshot, err := stores[0].Snapshot("worker")
	if err != nil || snapshot.Revision != 1 || len(snapshot.Events) != 1 {
		t.Fatalf("concurrent import snapshot=%+v %v", snapshot, err)
	}
}

func TestTranscriptSnapshotRowsAndRevisionStayConsistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	const writes = 100
	if err := writer.ReplaceAll("worker", []Event{{Index: 1, Body: "1"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observed := make(chan int64, 1)
	done := make(chan error, 1)
	go func() {
		for revision := 2; revision <= writes; revision++ {
			// Do not let either side run entirely before the other. Once a
			// read sees this commit, the next write races subsequent reads.
			select {
			case seen := <-observed:
				if seen != int64(revision-1) {
					done <- fmt.Errorf("writer expected observation %d, got %d", revision-1, seen)
					return
				}
			case <-ctx.Done():
				done <- ctx.Err()
				return
			}
			var events []Event
			if revision%2 != 0 {
				events = []Event{{Index: 1, Body: fmt.Sprint(revision)}}
			}
			if err := writer.ReplaceAll("worker", events); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var lastObserved int64
	for lastObserved < writes {
		snapshot, err := reader.Snapshot("worker")
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Revision%2 == 0 {
			if len(snapshot.Events) != 0 {
				t.Fatalf("empty revision contains rows from another commit: %+v", snapshot)
			}
		} else if len(snapshot.Events) != 1 || snapshot.Events[0].Body != fmt.Sprint(snapshot.Revision) {
			t.Fatalf("rows and revision came from different commits: %+v", snapshot)
		}
		if snapshot.Revision != lastObserved {
			if snapshot.Revision != lastObserved+1 {
				t.Fatalf("reader missed a committed revision: last=%d snapshot=%+v", lastObserved, snapshot)
			}
			lastObserved = snapshot.Revision
			observed <- lastObserved
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
