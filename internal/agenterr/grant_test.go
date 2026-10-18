// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// ProtocolError is the shape of claudia's broker ProtocolError.
// The production type lives in claudia's broker package, which this
// module does not import. Code is the machine code. The sentence is not.
type ProtocolError struct {
	Code string
	Msg  string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	return "broker protocol: " + e.Code + ": " + e.Msg
}

// otherCode is a different type that also has a Code field. It must not
// be read as a broker refusal.
type otherCode struct {
	Code string
}

func (e *otherCode) Error() string { return "broker protocol: " + e.Code }

func TestNoteBrokerGrantReadsProtocolCodeNotTheSentence(t *testing.T) {
	pe := &ProtocolError{Code: "not_owner", Msg: "grant jevons is not owned by this connection"}
	noted := agenterr.NoteBrokerGrant(pe)
	if !agenterr.IsGrantRefusal(noted) {
		t.Fatal("protocol code not_owner must become a grant refusal")
	}
	if noted.Error() != pe.Error() {
		t.Fatalf("text = %q, want the broker sentence kept", noted.Error())
	}
	if agenterr.NotifyClass(noted) != "not_owner" {
		t.Fatalf("class = %q", agenterr.NotifyClass(noted))
	}
	if again := agenterr.NoteBrokerGrant(noted); again != noted {
		t.Fatal("a second read must keep the typed refusal")
	}

	sentence := errors.New(pe.Error())
	if agenterr.IsGrantRefusal(sentence) || agenterr.IsGrantRefusal(agenterr.NoteBrokerGrant(sentence)) {
		t.Fatal("a sentence that contains not_owner is not a grant refusal")
	}
	if agenterr.NotifyClass(agenterr.NoteBrokerGrant(sentence)) != "" {
		t.Fatal("a sentence has no notify class")
	}

	other := &ProtocolError{Code: "spawn_failed", Msg: "no"}
	if agenterr.IsGrantRefusal(agenterr.NoteBrokerGrant(other)) {
		t.Fatal("another protocol code is not a grant refusal")
	}
	if agenterr.IsGrantRefusal(agenterr.NoteBrokerGrant(&otherCode{Code: "not_owner"})) {
		t.Fatal("a Code field on a different type is not a broker refusal")
	}

	wrapped := fmt.Errorf("send failed: %w", pe)
	got := agenterr.NoteBrokerGrant(wrapped)
	if !agenterr.IsGrantRefusal(got) || got.Error() != wrapped.Error() {
		t.Fatalf("wrapped = %v refusal %v", got, agenterr.IsGrantRefusal(got))
	}
	if !agenterr.IsGrantRefusal(fmt.Errorf("outer: %w", got)) {
		t.Fatal("a wrap of the typed refusal must classify the same")
	}

	// sendWithRequestID wraps the receipt sentinel and the protocol error
	// together. errors.Unwrap does not see the second one.
	both := fmt.Errorf("%w: %w", errors.New("request receipt uncertain"), pe)
	notedBoth := agenterr.NoteBrokerGrant(both)
	if !agenterr.IsGrantRefusal(notedBoth) {
		t.Fatal("not_owner on the second side of a two-error wrap was missed")
	}
}

func TestIsFrameTooLargeReadsProtocolCode(t *testing.T) {
	pe := &ProtocolError{Code: "frame_too_large", Msg: "frame of 2000000 bytes exceeds the 1048576-byte line limit; it was dropped and the connection kept open"}
	if !agenterr.IsFrameTooLarge(pe) {
		t.Fatal("bare frame_too_large was not recognised")
	}
	if !agenterr.IsFrameTooLarge(fmt.Errorf("send failed: %w", pe)) {
		t.Fatal("a single wrap of frame_too_large was not recognised")
	}
	both := fmt.Errorf("%w: %w", errors.New("request receipt uncertain"), pe)
	if !agenterr.IsFrameTooLarge(both) {
		t.Fatal("frame_too_large on the second side of a two-error wrap was missed")
	}
	sentence := errors.New("broker protocol: malformed: message exceeds the 1048576-byte line limit")
	if agenterr.IsFrameTooLarge(sentence) {
		t.Fatal("a line-limit sentence was recognised")
	}
	malformed := &ProtocolError{Code: "malformed", Msg: "message exceeds the 1048576-byte line limit"}
	if agenterr.IsFrameTooLarge(malformed) || agenterr.IsFrameTooLarge(fmt.Errorf("%w: %w", errors.New("request receipt uncertain"), malformed)) {
		t.Fatal("code malformed was recognised")
	}
	if agenterr.IsFrameTooLarge(&otherCode{Code: "frame_too_large"}) {
		t.Fatal("a Code field on a different type was recognised")
	}
	if agenterr.IsFrameTooLarge(nil) {
		t.Fatal("nil was recognised")
	}
}

func TestGrantRefusalTypesAKnownInjection(t *testing.T) {
	if agenterr.GrantRefusal(nil) != nil {
		t.Fatal("nil stays nil")
	}
	raw := errors.New(`broker protocol: not_owner (name="jevons"): grant jevons is not owned by this connection`)
	noted := agenterr.GrantRefusal(raw)
	if !agenterr.IsGrantRefusal(noted) || noted.Error() != raw.Error() {
		t.Fatalf("injected = %v", noted)
	}
	if agenterr.GrantRefusal(noted) != noted {
		t.Fatal("a second mark must keep the typed refusal")
	}
	if agenterr.NoteBrokerGrant(noted) != noted {
		t.Fatal("the boundary read must keep a refusal the caller already marked")
	}
}
