// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"strings"
	"testing"
)

func TestOwnerGateSuccessfulRecordEmitsTypedQuestionOnlyOnSuccess(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	runBullseye = func(...string) (string, error) { return "ok: true", nil }
	s := New(t.TempDir(), nil, nil)
	res, err := s.handleOwnerGate(context.Background(), t720RecordReq(t.TempDir(), "T2"))
	if err != nil || res.IsError {
		t.Fatalf("record: %v %v", res, err)
	}
	if !strings.Contains(targetFileToolText(res), `"id":"owner-gate"`) || !strings.Contains(targetFileToolText(res), `"state":"open"`) {
		t.Fatalf("typed question missing: %s", targetFileToolText(res))
	}
	runBullseye = func(...string) (string, error) { return "refused", context.Canceled }
	res, err = s.handleOwnerGate(context.Background(), t720RecordReq(t.TempDir(), "T2"))
	if err != nil || !res.IsError || strings.Contains(targetFileToolText(res), `"id":"owner-gate"`) {
		t.Fatalf("failure created question: %v %v", res, err)
	}
}
