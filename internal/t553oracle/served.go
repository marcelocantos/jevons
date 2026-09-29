// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package t553oracle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// 🎯T505: the T553.1 end-to-end oracle proves the buildsnap path builds from
// committed HEAD, but it never looks at :13705 itself. These helpers let a
// read-only probe decide that what the development daemon actually serves is
// byte-identical to ui/bundle.zip at the commit the restart script recorded
// as served — so no working-tree edit anywhere can be what the owner's
// browser executes.

// ParseServedSHA returns the commit on the last "served <epoch> <sha> …" line
// that scripts/restart-jevonsd.sh appends to ~/.jevons/restart-jevonsd.served.
func ParseServedSHA(content string) (string, error) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		f := strings.Fields(lines[i])
		if len(f) >= 3 && f[0] == "served" {
			if !shaRE.MatchString(f[2]) {
				return "", fmt.Errorf("served line %q: %q is not a commit SHA", lines[i], f[2])
			}
			return f[2], nil
		}
	}
	return "", fmt.Errorf("no served line in %d line(s)", len(lines))
}

var (
	shaRE        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	localAssetRE = regexp.MustCompile(`(?:src|href)="(/[^"]+)"`)
)

// ServedAssetPaths lists every local URL a served index.html references
// (scripts, stylesheets, icons), deduplicated in document order.
func ServedAssetPaths(indexHTML string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range localAssetRE.FindAllStringSubmatch(indexHTML, -1) {
		p := m[1]
		if strings.HasPrefix(p, "//") || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ServedMatchesBundle fetches GET / and every local asset it references
// through get, and requires each to be byte-identical to the same member of
// bundleZip. It returns the paths it compared. A served page that references
// no script cannot have been compared, so it is an error rather than a pass.
func ServedMatchesBundle(bundleZip []byte, get func(path string) ([]byte, error)) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(bundleZip), int64(len(bundleZip)))
	if err != nil {
		return nil, fmt.Errorf("committed bundle: %w", err)
	}
	member := func(name string) ([]byte, error) {
		f, err := zr.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}

	index, err := get("/")
	if err != nil {
		return nil, fmt.Errorf("GET /: %w", err)
	}
	want, err := member("index.html")
	if err != nil {
		return nil, fmt.Errorf("committed bundle has no index.html: %w", err)
	}
	if !bytes.Equal(index, want) {
		return nil, fmt.Errorf("GET / (%d bytes) differs from the committed index.html (%d bytes)", len(index), len(want))
	}
	checked := []string{"/"}
	scripts := 0
	for _, p := range ServedAssetPaths(string(index)) {
		want, err := member(strings.TrimPrefix(p, "/"))
		if err != nil {
			return checked, fmt.Errorf("served page references %s, which the committed bundle lacks", p)
		}
		got, err := get(p)
		if err != nil {
			return checked, fmt.Errorf("GET %s: %w", p, err)
		}
		if !bytes.Equal(got, want) {
			return checked, fmt.Errorf("GET %s (%d bytes) differs from the committed bundle member (%d bytes)", p, len(got), len(want))
		}
		if strings.HasSuffix(p, ".js") {
			scripts++
		}
		checked = append(checked, p)
	}
	if scripts == 0 {
		return checked, fmt.Errorf("served page references no script; nothing executable was compared")
	}
	return checked, nil
}
