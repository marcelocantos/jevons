// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "testing"

func TestBrokerSeatGoneRecognizesClaudiaPrefix(t *testing.T) {
	cause := ExitCauseBrokerSeatGonePrefix + "process exited"
	if !BrokerSeatGone(cause) {
		t.Fatalf("seat gone cause not recognized: %q", cause)
	}
	if !BrokerCaused(cause) {
		t.Fatal("BrokerCaused must include seat gone")
	}
	if BrokerPlanned(cause) {
		t.Fatal("seat gone is not a planned broker restart")
	}
}

func TestBrokerCausedRejectsQuotedSeatGoneProse(t *testing.T) {
	if BrokerCaused("seat process gone") {
		t.Fatal("log prose is not a broker exit code")
	}
}
