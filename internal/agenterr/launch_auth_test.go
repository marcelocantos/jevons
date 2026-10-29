// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestT607LaunchAuthFailurePinsIncidentAndCause(t *testing.T) {
	incident := errors.New("acp session/new: Authentication required")
	if !LaunchAuthFailure(incident) {
		t.Fatal("incident string must classify as launch auth")
	}
	caused := WithCause(CauseAuth, errors.New("broker refused"))
	if !LaunchAuthFailure(caused) {
		t.Fatal("typed CauseAuth must classify")
	}
	if LaunchAuthFailure(fmt.Errorf("connection refused")) {
		t.Fatal("transient backend must not classify as launch auth")
	}
	if LaunchAuthFailure(nil) {
		t.Fatal("nil is not auth")
	}
}

func TestT607LaunchAuthOwnerCopyNamesFix(t *testing.T) {
	msg := LaunchAuthOwnerCopy("acp session/new: Authentication required")
	if msg == "" || (!strings.Contains(strings.ToLower(msg), "grok") && !strings.Contains(strings.ToLower(msg), "api key")) {
		t.Fatalf("owner copy must name fix path, got: %q", msg)
	}
}
