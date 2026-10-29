// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"reflect"
	"testing"
)

func TestT418AgentsToStopForMute(t *testing.T) {
	got := t418AgentsToStopForMute([]string{"jv-t418h-1", "jv-t418w-2"})
	want := []string{"jv-t418h-1", "jv-t418w-2", overseerName}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	planted := []string{"only-seat"}
	_ = t418AgentsToStopForMute(planted)
	if len(planted) != 1 {
		t.Fatalf("mutated planted input: %v", planted)
	}
}
