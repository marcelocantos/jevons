// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPathUsesTheSameDecoderAsRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	if err := os.WriteFile(path, []byte(grokFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	turns, err := ReadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) < 4 {
		t.Fatalf("turns=%d: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !strings.Contains(turns[0].Text, "spawn a worker") {
		t.Fatalf("turn0: %+v", turns[0])
	}
}
