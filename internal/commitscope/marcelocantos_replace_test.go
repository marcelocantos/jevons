// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

func TestMarcelocantosReplaceCommitRefusal(t *testing.T) {
	head := []byte("module m\n\ngo 1.26\n")
	staged := []byte("module m\n\ngo 1.26\n\nreplace github.com/marcelocantos/claudia => ../claudia\n")
	refuse, msg := MarcelocantosReplaceCommitRefusal(head, staged)
	if !refuse {
		t.Fatal("expected refusal for new replace")
	}
	if !strings.Contains(msg, "T448") || !strings.Contains(msg, "../claudia") {
		t.Fatalf("message: %q", msg)
	}
	refuse, _ = MarcelocantosReplaceCommitRefusal(staged, staged)
	if refuse {
		t.Fatal("unchanged replace should not refuse")
	}
}
