// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"path/filepath"
	"sync"
	"testing"
)

// Receipt reconciliation relies on Append returning either durable success
// or an error while daemon shutdown may close the same journal.
func TestAppendConcurrentClose(t *testing.T) {
	const contenders = 8
	j, err := Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range contenders {
		wg.Go(func() {
			<-start
			// Both success before Close and refusal after it are valid.
			_ = j.Append(Event{Decision: "receipt_observed"})
		})
	}
	wg.Go(func() {
		<-start
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	close(start)
	wg.Wait()
	if err := j.Append(Event{Decision: "too_late"}); err == nil {
		t.Fatal("closed journal accepted receipt evidence")
	}
}
