// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIntegratePrereqAcceptsScriptAndSubmodules(t *testing.T) {
	p, err := parseIntegratePrereq("submodules: true\nscript: c/vendor/build.sh\nargs: [libsodium]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Submodules || p.Script != "c/vendor/build.sh" || len(p.Args) != 1 || p.Args[0] != "libsodium" {
		t.Fatalf("got %+v", p)
	}
}

func TestParseIntegratePrereqRejectsEscapeAndUnknownFields(t *testing.T) {
	for _, raw := range []string{
		"script: ../escape.sh\n",
		"script: /etc/passwd\n",
		"script: c/vendor/build.sh\nunknown: true\n",
		"args: [x]\n",
		"script: \"\"\n",
		"\n",
	} {
		if _, err := parseIntegratePrereq(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestParseIntegratePrereqSubmodulesOnly(t *testing.T) {
	p, err := parseIntegratePrereq("submodules: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Submodules || p.Script != "" {
		t.Fatalf("got %+v", p)
	}
}

func TestParseIntegratePrereqAcceptsTestTagsOnly(t *testing.T) {
	p, err := parseIntegratePrereq("test_tags: [csdke2e]\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Script != "" || p.Submodules || len(p.TestTags) != 1 || p.TestTags[0] != "csdke2e" {
		t.Fatalf("got %+v", p)
	}
}

func TestParseIntegratePrereqAcceptsTestTagsWithScript(t *testing.T) {
	p, err := parseIntegratePrereq("script: c/vendor/build.sh\ntest_tags: [csdke2e, integration]\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Script != "c/vendor/build.sh" || len(p.TestTags) != 2 {
		t.Fatalf("got %+v", p)
	}
	if joinGoTestTags(p.TestTags) != "csdke2e,integration" {
		t.Fatalf("joined tags = %q", joinGoTestTags(p.TestTags))
	}
}

func TestParseIntegratePrereqRejectsBadTestTags(t *testing.T) {
	for _, raw := range []string{
		"test_tags: [\"\"]\n",
		"test_tags: [\"bad tag\"]\n",
		"test_tags: [\"-evil\"]\n",
		"test_tags: [csdke2e, csdke2e]\n",
		"test_tags: [\"foo,bar\"]\n",
	} {
		if _, err := parseIntegratePrereq(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestPostMergeTestArgvHonoursDeclaredTags(t *testing.T) {
	untagged := postMergeTestArgv([]string{"c/test/csdke2e"}, nil)
	if len(untagged) == 0 {
		t.Fatal("untagged argv empty")
	}
	for _, a := range untagged {
		if a == "-tags" {
			t.Fatalf("undeclared tags leaked into argv: %q", untagged)
		}
	}
	tagged := postMergeTestArgv([]string{"c/test/csdke2e"}, []string{"csdke2e"})
	joined := strings.Join(tagged, " ")
	if !strings.Contains(joined, "-tags csdke2e") {
		t.Fatalf("want -tags csdke2e in %q", joined)
	}
	if !strings.Contains(joined, "./c/test/csdke2e/...") {
		t.Fatalf("want package pattern in %q", joined)
	}
}

func TestConfinedJoinStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	abs, err := confinedJoin(root, "scripts/gen.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		t.Fatalf("joined path %q is not under %q", abs, root)
	}
	if _, err := confinedJoin(root, ".."+string(os.PathSeparator)+"x"); err == nil {
		t.Fatal("escaped .. was accepted")
	}
}

func TestIntegratePrereqRunCreatesFile(t *testing.T) {
	wt := t.TempDir()
	script := filepath.Join(wt, "scripts", "gen.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nset -e\nmkdir -p generated\nprintf 'ok %s\\n' \"$1\" > generated/out.txt\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &integratePrereq{Script: "scripts/gen.sh", Args: []string{"arg1"}}
	if err := p.run(wt); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(wt, "generated", "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok arg1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIntegratePrereqRunFailureWraps(t *testing.T) {
	wt := t.TempDir()
	script := filepath.Join(wt, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := (&integratePrereq{Script: "fail.sh"}).run(wt)
	if !errors.Is(err, ErrIntegratePrereq) {
		t.Fatalf("want ErrIntegratePrereq, got %v", err)
	}
	if !strings.Contains(err.Error(), "fail.sh") {
		t.Fatalf("error does not name the script: %v", err)
	}
}
