// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildident_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/buildident"
)

// 🎯T993: the id is a prefix of the file's sha256, so the same bytes always
// name the same build and any changed byte names a different one.
func TestBinaryOfIsSHA256Prefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jevonsd")
	body := []byte("not really a binary\n")
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := buildident.BinaryOf(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])[:buildident.BinaryIDLen]
	if got != want {
		t.Fatalf("BinaryOf=%q want %q", got, want)
	}
	if len(got) != buildident.BinaryIDLen {
		t.Fatalf("len=%d want %d", len(got), buildident.BinaryIDLen)
	}

	again, err := buildident.BinaryOf(path)
	if err != nil || again != got {
		t.Fatalf("same bytes must name the same build: %q vs %q (err %v)", again, got, err)
	}

	if err := os.WriteFile(path, append(body, '!'), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := buildident.BinaryOf(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed == got {
		t.Fatalf("a changed binary must name a different build, both %q", got)
	}
}

func TestBinaryOfMissingFileErrors(t *testing.T) {
	if _, err := buildident.BinaryOf(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing file must error, not name a build")
	}
}

// The running process names itself once and keeps that answer: the test
// binary is a readable executable, so the id is non-empty and stable.
func TestBinaryIsStableWithinProcess(t *testing.T) {
	first := buildident.Binary()
	if first == "" {
		t.Fatal("Binary() empty for a readable test executable")
	}
	if len(first) != buildident.BinaryIDLen {
		t.Fatalf("len=%d want %d", len(first), buildident.BinaryIDLen)
	}
	if second := buildident.Binary(); second != first {
		t.Fatalf("Binary() changed within one process: %q then %q", first, second)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	direct, err := buildident.BinaryOf(exe)
	if err != nil {
		t.Fatal(err)
	}
	if direct != first {
		t.Fatalf("Binary()=%q but BinaryOf(os.Executable())=%q", first, direct)
	}
}
