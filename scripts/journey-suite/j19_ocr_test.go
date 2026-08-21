// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestJ19EmptyOCRFail(t *testing.T) {
	if !j19EmptyOCRFail(24, "") {
		t.Fatal("empty OCR + seeded model must fail")
	}
	if !j19EmptyOCRFail(16, "   ") {
		t.Fatal("whitespace OCR is empty")
	}
	if j19EmptyOCRFail(24, "ROOThist-23") {
		t.Fatal("distinctive token is not an empty pane")
	}
	if j19EmptyOCRFail(0, "") {
		t.Fatal("empty model is not the empty-pane bug")
	}
}
