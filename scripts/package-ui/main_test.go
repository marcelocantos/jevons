// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPackIncludesExactInputsAndIgnoresModificationTimes(t *testing.T) {
	source := t.TempDir()
	output := filepath.Join(t.TempDir(), "bundle.zip")
	inputs := map[string]string{
		"index.html":     "<div id=\"root\"></div>",
		"assets/app.js":  "app();",
		"assets/app.css": "body {}",
		"assets/lazy.js": "export default 42;",
		"favicon.svg":    "<svg/>",
	}
	for name, body := range inputs {
		file := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := pack(source, output, false); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != len(inputs) {
		t.Fatalf("archive has %d files, source has %d", len(archive.File), len(inputs))
	}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		want, exists := inputs[file.Name]
		if !exists || string(body) != want {
			t.Errorf("archive input %q differs from source", file.Name)
		}
		if file.Mode().Perm() != 0o644 {
			t.Errorf("archive mode %q = %v", file.Name, file.Mode())
		}
	}
	for name := range inputs {
		if err := os.Chtimes(filepath.Join(source, filepath.FromSlash(name)), time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := pack(source, output, true); err != nil {
		t.Fatalf("mtime changed deterministic bytes: %v", err)
	}
	// A changed source must fail check mode without silently regenerating.
	if err := os.WriteFile(filepath.Join(source, "assets/app.js"), []byte("changed();"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pack(source, output, true); err == nil {
		t.Fatal("stale bundle was accepted")
	}
	still, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(first, still) {
		t.Fatal("check mode modified the tracked bundle")
	}
	if err := pack(source, output, false); err != nil {
		t.Fatal(err)
	}
	if err := pack(source, output, true); err != nil {
		t.Fatal(err)
	}
}

func TestPackRejectsMissingDocumentAndSymlinks(t *testing.T) {
	source := t.TempDir()
	output := filepath.Join(t.TempDir(), "bundle.zip")
	if err := pack(source, output, false); err == nil {
		t.Fatal("missing React document was accepted")
	}
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("document"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(source, "index.html"), filepath.Join(source, "link.html")); err != nil {
		t.Fatal(err)
	}
	if err := pack(source, output, false); err == nil {
		t.Fatal("symlink bundle input was accepted")
	}
}
