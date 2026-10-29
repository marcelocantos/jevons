// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package superviselog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCapturedPathMatchesSupervisorTemplate(t *testing.T) {
	want := filepath.Join("/tmp/home", ".local", "var", "log", "jevonsd.log")
	if got := CapturedPath("/tmp/home"); got != want {
		t.Fatalf("CapturedPath=%q want %q", got, want)
	}
}

func TestSlogWriterSyncsWhenSupervised(t *testing.T) {
	t.Setenv("SUPERVISOR_ENABLED", "1")
	path := filepath.Join(t.TempDir(), "jevonsd.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := SlogWriter(f)
	if _, err := w.Write([]byte("boot line\n")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "boot line\n" {
		t.Fatalf("got %q", b)
	}
}

func TestSlogWriterPassthroughWhenNotSupervised(t *testing.T) {
	t.Setenv("SUPERVISOR_ENABLED", "")
	path := filepath.Join(t.TempDir(), "jevonsd.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	w := SlogWriter(f)
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if w == nil {
		t.Fatal("nil writer")
	}
	_ = f.Close()
}
