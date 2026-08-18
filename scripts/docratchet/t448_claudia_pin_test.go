// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Minimum published claudia the fleet's Goal ingest (🎯T510) needs.
// ../go.work will hide a stale pin during local builds; this test
// reads go.mod and resolves with GOWORK=off (🎯T448).
const minClaudiaPin = "v0.24.0"

func TestT448ClaudiaPinResolvesWithoutGoWork(t *testing.T) {
	root := repoRoot(t)
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	pin := claudiaRequire(string(mod))
	if pin == "" {
		t.Fatal("go.mod has no github.com/marcelocantos/claudia require")
	}
	if cmpSemver(pin, minClaudiaPin) < 0 {
		t.Fatalf("go.mod pins claudia %s, want >= %s (go.work is not the pin)", pin, minClaudiaPin)
	}

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}} {{.Dir}}", "github.com/marcelocantos/claudia")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("GOWORK=off go list: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		t.Fatalf("GOWORK=off go list: %q", out)
	}
	got, dir := fields[0], fields[1]
	if got != pin {
		t.Fatalf("GOWORK=off resolved %s, go.mod pin is %s", got, pin)
	}
	if strings.Contains(dir, filepath.Join("github.com", "marcelocantos", "claudia") ) && !strings.Contains(dir, "@") {
		t.Fatalf("GOWORK=off still using the sibling checkout %s — pin did not resolve to the module cache", dir)
	}
	if !strings.Contains(dir, "@"+pin) {
		t.Fatalf("GOWORK=off Dir = %s, want module cache @%s", dir, pin)
	}
}

func claudiaRequire(mod string) string {
	for _, line := range strings.Split(mod, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "github.com/marcelocantos/claudia ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return fields[1]
		}
	}
	return ""
}

// cmpSemver compares vA and vB as vMAJOR.MINOR.PATCH. Returns -1, 0, 1.
func cmpSemver(a, b string) int {
	pa := semverParts(a)
	pb := semverParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func semverParts(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.Split(v, ".") {
		if i >= 3 {
			break
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out[i] = n
	}
	return out
}
