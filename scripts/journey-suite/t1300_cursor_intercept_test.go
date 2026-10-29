// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia/omp"
)

func t1300Testdata(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

func t1300StageSidecar(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDIA_OMP_SERVER", "")
	src := filepath.Dir(omp.ServerScript())
	dst := t.TempDir()
	if err := copyIsolatedSidecarSource(src, dst); err != nil {
		t.Fatal(err)
	}
	td := t1300Testdata(t)
	for _, name := range []string{"t1300-cursor-catalog.ts", "t1300-cursor-intercept.test.ts", "replay-core.ts", "replay-core.test.ts"} {
		body, err := os.ReadFile(filepath.Join(td, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if err := omp.EnsureDeps(ctx, filepath.Join(dst, "server.ts")); err != nil {
		t.Fatal(err)
	}
	return dst
}

// t1300BunRun runs one bun test file in dir and returns its console output
// and the names of the cases that passed, read from bun's JUnit report.
// bun lists passing case names on the console only in some environments
// (not in make gate's clean checkout), so the report is what proves which
// cases ran.
func t1300BunRun(t *testing.T, dir, file string) (string, map[string]bool, error) {
	t.Helper()
	report := filepath.Join(t.TempDir(), "junit.xml")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", "test", "--reporter=junit", "--reporter-outfile="+report, file)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	passed := map[string]bool{}
	raw, err := os.ReadFile(report)
	if err != nil {
		return out.String(), passed, runErr
	}
	// Cases nest at varying depth (file suite, describe suite), so walk tokens.
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var name string
	ok := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "testcase":
				name, ok = "", true
				for _, a := range el.Attr {
					if a.Name.Local == "name" {
						name = a.Value
					}
				}
			case "failure", "error", "skipped":
				ok = false
			}
		case xml.EndElement:
			if el.Name.Local == "testcase" && name != "" {
				passed[name] = ok
			}
		}
	}
	return out.String(), passed, runErr
}

// TestT1300CursorCatalogMockInterceptsModelTurns is 🎯T1300 Stage A.
// A Cursor seat constructed without modelOverride must take model turns from
// a planted catalog mock (abort-provider shape). Native http2 is a bypass:
// stop and file a Claudia target rather than work around it from Jevons.
func TestT1300CursorCatalogMockInterceptsModelTurns(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Fatal("Stage A needs bun on PATH (the journey isolate already does)")
	}
	dst := t1300StageSidecar(t)
	body, passed, err := t1300BunRun(t, dst, "t1300-cursor-intercept.test.ts")
	t.Log(body)
	if strings.Contains(body, "T1300 BYPASS") {
		t.Fatalf("Stage A DISPROVEN: Cursor model turns bypass getBundledModel and hit native cursor-agent http2/fetch.\nFile a Claudia target for a first-class replay hook; do not work around this from Jevons.\n%s", body)
	}
	if err != nil {
		t.Fatalf("Stage A bun test: %v\n%s", err, body)
	}
	for _, c := range []string{
		"composer-2.5 model turns hit the planted mock, not http2",
		"a prompt that is not in the cassette fails stale, never live",
	} {
		if !passed[c] {
			t.Fatalf("Stage A intercept case %q did not pass (passed: %v):\n%s", c, passed, body)
		}
	}
}

// TestT1300ReplayCore pins Stage B against the frozen sidecar dependency set.
func TestT1300ReplayCore(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Fatal(err)
	}
	out, passed, err := t1300BunRun(t, t1300Testdata(t), "replay-core.test.ts")
	t.Log(out)
	if err != nil {
		t.Fatalf("Stage B replay hermetic: %v: %s", err, out)
	}
	for _, c := range []string{
		"canonical substitutions, and authored target/brief preservation",
		"source drift fails before first model call",
		"exact turn replays; miss, exhausted and unused entries fail stale, never live",
	} {
		if !passed[c] {
			t.Fatalf("Stage B case %q did not pass (passed: %v):\n%s", c, passed, out)
		}
	}
}
