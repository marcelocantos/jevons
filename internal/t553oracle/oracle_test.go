// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package t553oracle

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkZip(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.CreateHeader(&zip.FileHeader{Name: "assets/a.js", Method: zip.Deflate})
	f.Write([]byte(body))
	w.Close()
	return buf.Bytes()
}

func TestZipContainsSeesInsideDeflate(t *testing.T) {
	m := NewMarker()
	z := mkZip(t, strings.Repeat("x", 4000)+`throw new Error("`+m+`")`)
	if BytesContain(z, m) {
		t.Fatal("raw scan should not see a deflated marker; fixture is not exercising the trap")
	}
	if ok, err := ZipContains(z, m); err != nil || !ok {
		t.Fatalf("ZipContains = %v, %v; want true", ok, err)
	}
	if ok, _ := ZipContains(mkZip(t, "clean"), m); ok {
		t.Fatal("clean zip reported the marker")
	}
}

func TestServedScriptPaths(t *testing.T) {
	got := ServedScriptPaths(`<script type="module" src="/assets/index-1.js"></script><link rel="modulepreload" href="/assets/v-2.js"><link href="/x.css"><script src="/assets/index-1.js">`)
	if len(got) != 2 || got[0] != "/assets/index-1.js" || got[1] != "/assets/v-2.js" {
		t.Fatalf("got %v", got)
	}
}

func TestJudge(t *testing.T) {
	yes, no := Verdict{"c", true}, Verdict{"s", false}
	if err := Judge([]Verdict{yes}, []Verdict{no}); err != nil {
		t.Fatalf("good case: %v", err)
	}
	if Judge([]Verdict{{"c", false}}, []Verdict{no}) == nil {
		t.Fatal("control without the throw must fail (vacuous oracle)")
	}
	if Judge([]Verdict{yes}, []Verdict{{"s", true}}) == nil {
		t.Fatal("snapshot carrying the throw must fail")
	}
	if Judge(nil, []Verdict{no}) == nil || Judge([]Verdict{yes}, nil) == nil {
		t.Fatal("empty side must fail")
	}
}

func TestSeedThrowRefusesNonScratchTree(t *testing.T) {
	dir := t.TempDir() // path lacks "t553-oracle"
	if err := SeedThrow(dir, "M"); err == nil {
		t.Fatal("SeedThrow must refuse a tree outside a t553-oracle scratch dir")
	}
}

func TestSeedThrowWritesUncommittedEdits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "t553-oracle-x")
	os.MkdirAll(filepath.Join(dir, "ui/src"), 0o755)
	os.MkdirAll(filepath.Join(dir, "cmd/jevonsd"), 0o755)
	os.WriteFile(filepath.Join(dir, UIThrowPath), []byte("export {}\n"), 0o644)
	m := NewMarker()
	if err := SeedThrow(dir, m); err != nil {
		t.Fatal(err)
	}
	ui, _ := os.ReadFile(filepath.Join(dir, UIThrowPath))
	gs, _ := os.ReadFile(filepath.Join(dir, GoThrowPath))
	if !strings.Contains(string(ui), m) || !strings.Contains(string(gs), m) {
		t.Fatal("marker missing from a seeded file")
	}
}
