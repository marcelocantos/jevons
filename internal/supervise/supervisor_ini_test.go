// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func supervisorDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "supervisor")
	if _, err := os.Stat(filepath.Join(dir, "install.sh")); err != nil {
		t.Fatalf("supervisor/ missing from repo: %v", err)
	}
	return dir
}

func TestSupervisorTemplatesAreVellumShaped(t *testing.T) {
	dir := supervisorDir(t)
	for _, name := range []string{"jevonsd.ini", "jevons-vanilla.ini"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(body)
		for _, want := range []string{
			"@REPO@",
			"command=@REPO@/supervisor/run-",
			"directory=@REPO@",
			"autostart=true",
			"autorestart=true",
			"%(ENV_HOME)s/.local/var/log/",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s missing %q\n%s", name, want, s)
			}
		}
		if strings.Contains(s, "5173") || strings.Contains(s, "npm run") {
			t.Errorf("%s must not be the Vite :5173 agent", name)
		}
		if strings.Contains(s, "restart-daily-jevonsd") {
			t.Errorf("%s must not invoke the fat restart script", name)
		}
	}
	d, err := os.ReadFile(filepath.Join(dir, "jevonsd.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d), "[program:jevonsd]") {
		t.Fatal("jevonsd.ini must name program:jevonsd")
	}
	v, err := os.ReadFile(filepath.Join(dir, "jevons-vanilla.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v), "[program:jevons-vanilla]") {
		t.Fatal("jevons-vanilla.ini must name program:jevons-vanilla")
	}
}

func TestSupervisorInstallRendersRepoRoot(t *testing.T) {
	dir := supervisorDir(t)
	conf := t.TempDir()
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "install.sh"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_SKIP_CTL=1",
		"HOME="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	repo := filepath.Clean(filepath.Join(dir, ".."))
	for _, name := range []string{"jevonsd.ini", "jevons-vanilla.ini"} {
		body, err := os.ReadFile(filepath.Join(conf, name))
		if err != nil {
			t.Fatalf("rendered %s: %v", name, err)
		}
		s := string(body)
		if strings.Contains(s, "@REPO@") {
			t.Fatalf("%s still has @REPO@:\n%s", name, s)
		}
		if !strings.Contains(s, repo+"/supervisor/run-") {
			t.Fatalf("%s command not expanded to %s:\n%s", name, repo, s)
		}
	}
	jevonsd, _ := os.ReadFile(filepath.Join(conf, "jevonsd.ini"))
	if strings.Contains(string(jevonsd), "/opt/homebrew/opt/jevons") {
		t.Fatal("rendered jevonsd must not point at Cellar")
	}
}
