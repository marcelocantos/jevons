// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// A bundle whose files match but whose compressed bytes differ, as when a
// different Go release deflated it, passes the check and is not rewritten.
// The v0.16.0 release CI (go1.26.1) failed a bundle built with go1.27.1
// whose every file was identical.
func TestCheckComparesContentsNotCompressedBytes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dist")
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"index.html": "<html></html>", "assets/app.js": strings.Repeat("console.log(1);\n", 200)}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The same entries, stored uncompressed: different archive bytes.
	var stored bytes.Buffer
	w := zip.NewWriter(&stored)
	for _, name := range []string{"assets/app.js", "index.html"} {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0o644)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(out, stored.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pack(src, out, true); err != nil {
		t.Fatalf("check failed on identical contents: %v", err)
	}
	if err := pack(src, out, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, stored.Bytes()) {
		t.Fatal("an identical bundle was rewritten")
	}
	// A real content change still fails the check.
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<html>new</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pack(src, out, true); err == nil {
		t.Fatal("check passed a bundle whose contents changed")
	}
}
