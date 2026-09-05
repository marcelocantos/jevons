// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// package-ui writes the deterministic, tracked React bundle embedded by jevonsd.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
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
	if previous, err := os.ReadFile(output); err == nil && bytes.Equal(previous, buf.Bytes()) {
		return nil
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
