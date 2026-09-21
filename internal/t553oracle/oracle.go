// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package t553oracle holds the pure pieces of the 🎯T553.1 oracle: the
// development daemon executes committed HEAD, never the shared clone's
// working tree. The end-to-end test (oracle_test.go, gated by
// JEVONS_T553_ORACLE=1, `make test-t553-oracle`) seeds a deliberate throw
// into an ISOLATED clone's uncommitted tree — never the live shared clone —
// builds through the same buildsnap path restart-daily-jevonsd.sh uses, and
// scans what that build serves. These helpers decide what "contains the
// throw" means so the decision is pinned hermetically.
package t553oracle

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GoThrowPath is the untracked file SeedThrow adds. It panics at daemon
// start only when ArmEnv is set, so the control daemon (built from the
// working tree) can still boot and serve its bundle.
const (
	GoThrowPath = "cmd/jevonsd/zz_t553_seeded_throw.go"
	UIThrowPath = "ui/src/main.tsx"
	ArmEnv      = "T553_ARM_SEEDED_THROW"
)

// NewMarker returns a unique, greppable throw string.
func NewMarker() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "T553_SEEDED_THROW_" + hex.EncodeToString(b[:])
}

// SeedThrow writes the deliberate throw into clone's working tree (UI edit to
// a tracked file plus a new untracked Go file). It refuses any dir that is
// not a scratch copy: the marker must never reach a tree other workers build.
func SeedThrow(clone, marker string) error {
	abs, err := filepath.Abs(clone)
	if err != nil {
		return err
	}
	if !strings.Contains(filepath.Base(filepath.Dir(abs))+filepath.Base(abs), "t553-oracle") &&
		!strings.Contains(abs, "t553-oracle") {
		return fmt.Errorf("refusing to seed a throw into %s: not a t553-oracle scratch path", abs)
	}
	ui := filepath.Join(abs, UIThrowPath)
	f, err := os.OpenFile(ui, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", ui, err)
	}
	_, werr := fmt.Fprintf(f, "\nthrow new Error(%q);\n", marker)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	goSrc := fmt.Sprintf(`package main

import "os"

func init() {
	if os.Getenv(%q) != "" {
		panic(%q)
	}
}
`, ArmEnv, marker)
	return os.WriteFile(filepath.Join(abs, GoThrowPath), []byte(goSrc), 0o644)
}

// BytesContain reports whether the marker is in raw bytes (Go binary strings
// are stored uncompressed).
func BytesContain(b []byte, marker string) bool { return bytes.Contains(b, []byte(marker)) }

// ZipContains reports whether any member of the zip holds the marker. A
// bundle is deflated, so a raw scan of it proves nothing.
func ZipContains(zipBytes []byte, marker string) (bool, error) {
	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return false, err
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return false, err
		}
		var buf bytes.Buffer
		_, err = buf.ReadFrom(rc)
		rc.Close()
		if err != nil {
			return false, err
		}
		if bytes.Contains(buf.Bytes(), []byte(marker)) {
			return true, nil
		}
	}
	return false, nil
}

var assetRE = regexp.MustCompile(`(?:src|href)="(/[^"]+\.js)"`)

// ServedScriptPaths lists the JS URLs a served index.html references.
func ServedScriptPaths(indexHTML string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range assetRE.FindAllStringSubmatch(indexHTML, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Verdict classifies one scanned surface.
type Verdict struct {
	Surface string
	Found   bool
}

// Judge decides the oracle result. The oracle passes only when the control
// surfaces (working-tree builds) DO carry the throw — otherwise the scan
// could not fail and proves nothing — and every HEAD-snapshot surface does not.
func Judge(controls, snapshots []Verdict) error {
	if len(controls) == 0 || len(snapshots) == 0 {
		return fmt.Errorf("vacuous oracle: %d controls, %d snapshot surfaces", len(controls), len(snapshots))
	}
	for _, c := range controls {
		if !c.Found {
			return fmt.Errorf("control %q lacks the seeded throw: the scan cannot fail, so it proves nothing", c.Surface)
		}
	}
	for _, s := range snapshots {
		if s.Found {
			return fmt.Errorf("snapshot surface %q carries the seeded working-tree throw: daily is not committed HEAD", s.Surface)
		}
	}
	return nil
}
