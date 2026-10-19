// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudiawire

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAwaitReceiptReadsAgainOnlyWhileAbsent(t *testing.T) {
	q := ReceiptQuery{Seat: "s", SessionID: "g", RequestID: "r"}
	want := &RequestReceipt{Seat: "s", SessionID: "g", RequestID: "r", State: ReceiptAccepted}

	reads := 0
	got, err := AwaitReceipt(context.Background(), func(context.Context, ReceiptQuery) (*RequestReceipt, error) {
		if reads++; reads < 3 {
			return nil, fmt.Errorf("replicated %w", ErrReceiptAbsent)
		}
		return want, nil
	}, q)
	if err != nil || got != want || reads != 3 {
		t.Fatalf("got=%v err=%v reads=%d; want the receipt on the third read", got, err, reads)
	}

	refused := errors.New("database is locked")
	reads = 0
	if _, err := AwaitReceipt(context.Background(), func(context.Context, ReceiptQuery) (*RequestReceipt, error) {
		reads++
		return nil, refused
	}, q); !errors.Is(err, refused) || reads != 1 {
		t.Fatalf("err=%v reads=%d; an error other than absence is final", err, reads)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := AwaitReceipt(ctx, func(context.Context, ReceiptQuery) (*RequestReceipt, error) {
		return nil, fmt.Errorf("replicated %w", ErrReceiptAbsent)
	}, q); !errors.Is(err, ErrReceiptAbsent) {
		t.Fatalf("err=%v; a row that never arrives returns the absence at the deadline", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("waited %s past a 60ms deadline", waited)
	}
}
