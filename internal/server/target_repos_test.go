// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedTargetLookupCollisionAndRejection(t *testing.T) {
	old := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = old })
	root := t.TempDir()
	for slug, content := range map[string]string{
		"jevons":  "targets:\n  T177:\n    name: Column widths\n    status: achieved\n    acceptance: [width]\n",
		"claudia": "targets:\n  T177:\n    name: Purge legacy CLI transport\n    status: identified\n    acceptance: [remove CLI]\n  T177.2:\n    name: Dotted child\n    status: converging\n",
	} {
		dir := filepath.Join(root, slug)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runBullseyeCLI = func(args ...string) (string, error) {
		return "File: " + filepath.Join(args[len(args)-1], "bullseye.yaml") + "\n", nil
	}
	s := New("test", t.TempDir())
	s.SetFrontierCwd(filepath.Join(root, "jevons"))
	if err := s.SetTargetRepoRoots(root); err != nil {
		t.Fatal(err)
	}
	check := func(query string, wantCode int, wantName string) {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleFrontierTarget(rr, httptest.NewRequest(http.MethodGet, "/api/frontier/target?"+query, nil))
		if rr.Code != wantCode {
			t.Fatalf("%s: status %d body %s", query, rr.Code, rr.Body.String())
		}
		var got TargetResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if wantName != "" && (got.Target == nil || got.Target.Name != wantName) {
			t.Fatalf("%s: %+v", query, got)
		}
		if strings.Contains(query, "repo=claudia") && got.Found && (got.Repo != "claudia" || got.LedgerKey == "" || got.Target.ID == "") {
			t.Fatalf("missing scoped identity: %+v", got)
		}
		if wantCode != 200 && got.Found {
			t.Fatalf("failure fell back to a target: %+v", got)
		}
	}
	check("id=T177", 200, "Column widths")
	check("repo=claudia&id=T177&cwd="+filepath.Join(root, "jevons"), 200, "Purge legacy CLI transport")
	check("id=claudia%2FT177", 200, "Purge legacy CLI transport")
	check("repo="+filepath.Base(filepath.Dir(root))+"%2F"+filepath.Base(root)+"%2Fclaudia&id=T177", 200, "Purge legacy CLI transport")
	check("repo=claudia&id=T177.2", 200, "Dotted child")
	check("repo=unknown&id=T177", 404, "")
	check("repo=claudia&id=T999", 404, "")
	check("repo=..%2Fclaudia&id=T177", 400, "")
	check("repo=claudia&id=..%2FT177", 400, "")
	check("repo=claudia&id=T177%2F..", 400, "")
}

func TestScopedTargetAmbiguousSlugAndSymlinkIdentity(t *testing.T) {
	old := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = old })
	base := t.TempDir()
	roots := []string{filepath.Join(base, "a"), filepath.Join(base, "b")}
	for _, root := range roots {
		dir := filepath.Join(root, "same")
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte("targets:\n  T1:\n    name: One\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runBullseyeCLI = func(args ...string) (string, error) {
		return "File: " + filepath.Join(args[len(args)-1], "bullseye.yaml") + "\n", nil
	}
	s := New("test", t.TempDir())
	if err := s.SetTargetRepoRoots(roots...); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handleFrontierTarget(rr, httptest.NewRequest("GET", "/api/frontier/target?repo=same&id=T1", nil))
	if rr.Code != 409 {
		t.Fatalf("ambiguous: %d %s", rr.Code, rr.Body.String())
	}
	fq := filepath.Base(base) + "/a/same"
	rr = httptest.NewRecorder()
	s.handleFrontierTarget(rr, httptest.NewRequest("GET", "/api/frontier/target?repo="+fq+"&id=T1", nil))
	if rr.Code != 200 {
		t.Fatalf("fully qualified identity: %d %s", rr.Code, rr.Body.String())
	}

	// A symlink to an already-indexed checkout does not invent another ledger.
	link := filepath.Join(base, "alias")
	if err := os.Symlink(roots[0], link); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTargetRepoRoots(roots[0], link); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	s.handleFrontierTarget(rr, httptest.NewRequest("GET", "/api/frontier/target?repo=same&id=T1", nil))
	if rr.Code != 200 {
		t.Fatalf("alias: %d %s", rr.Code, rr.Body.String())
	}
}

func TestScopedTargetRepoSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "bullseye.yaml"), []byte("targets:\n  T177:\n    name: Escaped\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "claudia")); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	if err := s.SetTargetRepoRoots(root); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handleFrontierTarget(rr, httptest.NewRequest("GET", "/api/frontier/target?repo=claudia&id=T177", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("symlink escape admitted: %d %s", rr.Code, rr.Body.String())
	}
}
