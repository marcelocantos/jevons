// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gomodreplace

import "testing"

func TestMarcelocantosReplaceLines(t *testing.T) {
	body := "module m\n\ngo 1.26\n\nreplace github.com/marcelocantos/claudia => ../claudia\n"
	got := MarcelocantosReplaceLines(body)
	if len(got) != 1 || got[0] != "replace github.com/marcelocantos/claudia => ../claudia" {
		t.Fatalf("got %v", got)
	}
	block := "replace (\n\tgithub.com/marcelocantos/foo => ../foo\n)\n"
	if len(MarcelocantosReplaceLines(block)) != 1 {
		t.Fatal("block form")
	}
}

func TestNewMarcelocantosReplaceLines(t *testing.T) {
	head := "module m\n\ngo 1.26\n"
	staged := head + "\nreplace github.com/marcelocantos/claudia => ../claudia\n"
	if len(NewMarcelocantosReplaceLines(head, staged)) != 1 {
		t.Fatal("expected new replace")
	}
	unchanged := head + "\nreplace github.com/marcelocantos/claudia => ../claudia\n"
	headWith := unchanged
	if len(NewMarcelocantosReplaceLines(headWith, unchanged)) != 0 {
		t.Fatal("unchanged replace should not refuse")
	}
	changed := head + "\nreplace github.com/marcelocantos/claudia => /abs/claudia\n"
	if len(NewMarcelocantosReplaceLines(headWith, changed)) != 1 {
		t.Fatal("changed replace path should refuse")
	}
}
