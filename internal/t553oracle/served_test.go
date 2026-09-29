// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package t553oracle

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func bundleOf(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range members {
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const testIndex = `<!doctype html><link rel="icon" href="/favicon.svg"><script type="module" src="/assets/index-1.js"></script><link rel="stylesheet" href="/assets/index-1.css"><a href="//cdn.example/x.js">`

func committed(t *testing.T) (map[string]string, []byte) {
	m := map[string]string{
		"index.html":         testIndex,
		"favicon.svg":        "<svg/>",
		"assets/index-1.js":  "console.log('committed')",
		"assets/index-1.css": "body{}",
	}
	return m, bundleOf(t, m)
}

func serve(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		name := strings.TrimPrefix(p, "/")
		if name == "" {
			name = "index.html"
		}
		body, ok := files[name]
		if !ok {
			return nil, errors.New("404")
		}
		return []byte(body), nil
	}
}

func TestServedMatchesBundleAcceptsCommittedServe(t *testing.T) {
	files, zipBytes := committed(t)
	checked, err := ServedMatchesBundle(zipBytes, serve(files))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/", "/favicon.svg", "/assets/index-1.js", "/assets/index-1.css"}
	if strings.Join(checked, ",") != strings.Join(want, ",") {
		t.Fatalf("checked %v, want %v", checked, want)
	}
}

// The failure the oracle exists for: a working-tree edit (the 2026-08-18
// closeProviderMenu shape) is what the daemon serves, not the commit.
func TestServedMatchesBundleRejectsWorkingTreeScript(t *testing.T) {
	files, zipBytes := committed(t)
	wip := map[string]string{}
	for k, v := range files {
		wip[k] = v
	}
	wip["assets/index-1.js"] = "closeProviderMenu()"
	if _, err := ServedMatchesBundle(zipBytes, serve(wip)); err == nil || !strings.Contains(err.Error(), "/assets/index-1.js") {
		t.Fatalf("err = %v; want a mismatch naming /assets/index-1.js", err)
	}
}

func TestServedMatchesBundleRejectsForeignIndexAndMissingAsset(t *testing.T) {
	files, zipBytes := committed(t)
	other := map[string]string{"index.html": strings.Replace(testIndex, "index-1", "index-2", 2), "assets/index-2.js": "x", "assets/index-2.css": "y", "favicon.svg": "<svg/>"}
	if _, err := ServedMatchesBundle(zipBytes, serve(other)); err == nil || !strings.Contains(err.Error(), "GET /") {
		t.Fatalf("foreign index: err = %v", err)
	}
	delete(files, "assets/index-1.css")
	if _, err := ServedMatchesBundle(zipBytes, serve(files)); err == nil || !strings.Contains(err.Error(), "index-1.css") {
		t.Fatalf("missing served asset: err = %v", err)
	}
}

func TestServedMatchesBundleRefusesVacuousPage(t *testing.T) {
	m := map[string]string{"index.html": `<!doctype html><div id="root"></div>`}
	if _, err := ServedMatchesBundle(bundleOf(t, m), serve(m)); err == nil || !strings.Contains(err.Error(), "no script") {
		t.Fatalf("err = %v; want vacuous refusal", err)
	}
}

func TestParseServedSHA(t *testing.T) {
	const a = "996972abf522f642f2092db5be01c708b3fc84ad"
	const b = "25f9e220452810a3faf1f2de61d41a54456312ca"
	got, err := ParseServedSHA("served 1790335214 " + a + " coalesced=0 bounce=1\nserved 1790671554 " + b + " coalesced=2 bounce=1\n")
	if err != nil || got != b {
		t.Fatalf("got %q, %v; want the last line's %s", got, err, b)
	}
	if _, err := ParseServedSHA(""); err == nil {
		t.Fatal("empty file must not yield a SHA")
	}
	if _, err := ParseServedSHA("served 1 HEAD coalesced=0"); err == nil {
		t.Fatal("a symbolic ref is not a served commit")
	}
}
