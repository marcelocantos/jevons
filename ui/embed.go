// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ui provides the compiled React cockpit for standalone binaries.
package ui

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io/fs"
)

// bundle.zip is tracked so a pristine Go build needs no prior generator run.
// make ui-build regenerates it from the locked, type-checked React source.
//
//go:embed bundle.zip
var bundle []byte

func Files() (fs.FS, error) {
	return zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
}
