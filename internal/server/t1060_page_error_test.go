// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestT1060FailedStoragePageIsTypedErrorNotEOF(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetStateDB(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	buf := &replayBuf{}
	s.writeMuxPageBeforeDB(context.Background(), buf, nil, "jevons", "e:3", 50)
	if len(buf.frames) != 1 || buf.frames[0]["t"] != "error" {
		t.Fatalf("storage failure must be exactly one error, never a successful empty page/EOF: %+v", buf.frames)
	}
	body, ok := buf.frames[0]["body"].(map[string]any)
	if !ok || body["op"] != "page" || body["before"] != "e:3" {
		t.Fatalf("page error missing operation/cursor: %+v", buf.frames)
	}
	message, _ := body["error"].(string)
	if !strings.Contains(message, "Could not load") || strings.Contains(message, "sql") {
		t.Fatalf("error must be user-safe retry copy, not storage internals: %q", message)
	}
}
