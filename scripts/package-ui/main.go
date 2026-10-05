// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// package-ui writes the deterministic, tracked React bundle embedded by jevonsd.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	source := flag.String("source", "ui/dist", "Vite build directory")
	output := flag.String("output", "ui/bundle.zip", "tracked embedded bundle")
	check := flag.Bool("check", false, "fail if the tracked bundle differs; do not update it")
	flag.Parse()
	if err := pack(*source, *output, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pack(source, output string, check bool) error {
	if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
		return fmt.Errorf("React build missing: %w", err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("bundle input is not a regular file: %s", path)
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// WalkDir sorts names; zero timestamps and fixed modes avoid machine
		// metadata changing the committed archive after an identical build.
		header := &zip.FileHeader{Name: filepath.ToSlash(name), Method: zip.Deflate}
		header.SetMode(0o644)
		file, err := w.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	// Compare what the archive holds, not its compressed bytes: two Go
	// releases deflate identical files differently, so a bundle built with
	// go1.27 never byte-matched the go1.26 release runner (v0.16.0 release
	// CI, 2026-10-05) although every file in it was the same. An existing
	// bundle with the same contents is also left as it is, so a toolchain
	// change alone never rewrites the tracked file.
	if previous, err := os.ReadFile(output); err == nil {
		same, err := sameContents(previous, buf.Bytes())
		if err != nil {
			return fmt.Errorf("read %s: %w", output, err)
		}
		if same {
			return nil
		}
	}
	if check {
		return fmt.Errorf("%s differs from the canonical React build; run make ui-build and commit the bundle with its source", output)
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".bundle-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), output)
}

// sameContents reports whether two bundles hold the same files, in the same
// order, with the same modes and bytes.
func sameContents(a, b []byte) (bool, error) {
	ra, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		return false, err
	}
	rb, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return false, err
	}
	if len(ra.File) != len(rb.File) {
		return false, nil
	}
	for i, fa := range ra.File {
		fb := rb.File[i]
		if fa.Name != fb.Name || fa.Mode() != fb.Mode() {
			return false, nil
		}
		da, err := readEntry(fa)
		if err != nil {
			return false, err
		}
		db, err := readEntry(fb)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(da, db) {
			return false, nil
		}
	}
	return true, nil
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
