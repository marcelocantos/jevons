// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/supervise"
)

func t1086Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The 2026-10-20 shape: a Homebrew-style main config whose [include] pulls
// in supervisor.d/*.ini, and a paused jevonsd.ini.
func TestT1086ReadsThePausedPolicyThroughInclude(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "supervisord.conf")
	t1086Write(t, conf, `; main config
[unix_http_server]
file=/tmp/x.sock   ; inline comment

[include]
files = supervisor.d/*.ini
    %(here)s/extra/*.conf
`)
	t1086Write(t, filepath.Join(dir, "supervisor.d", "claudia.ini"), "[program:claudia]\ncommand=/bin/true\nautorestart=true\n")
	ini := filepath.Join(dir, "supervisor.d", "jevonsd.ini")
	t1086Write(t, ini, `[program:jevonsd]
command=/repo/supervisor/run-jevonsd.sh
environment=HOME="%(ENV_HOME)s",PATH="/a:/b"
autostart=false
autorestart=false ; paused 2026-10-15
`)
	t1086Write(t, filepath.Join(dir, "extra", "other.conf"), "[program:other]\nautorestart=unexpected\n")

	p, err := supervise.SupervisordProgramPolicy(conf, "jevonsd")
	if err != nil {
		t.Fatal(err)
	}
	if p.Autorestart != "false" || p.Autostart != "false" || p.File != ini || p.Relaunches() {
		t.Fatalf("policy = %+v", p)
	}
	if got := p.String(); got != "autorestart=false autostart=false file="+ini {
		t.Fatalf("String() = %q", got)
	}

	other, err := supervise.SupervisordProgramPolicy(conf, "other")
	if err != nil {
		t.Fatal(err)
	}
	if other.Autorestart != "unexpected" || other.Relaunches() {
		t.Fatalf("continuation-line include not followed: %+v", other)
	}
}

// supervisord's default is autorestart=unexpected, which does not relaunch
// an upgrade exit's expected status. Reading "unset" as "relaunches" would
// put the 2026-10-20 outage back.
func TestT1086DefaultAutorestartDoesNotRelaunch(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "supervisord.conf")
	t1086Write(t, conf, "[program:jevonsd]\ncommand=/bin/true\n")
	p, err := supervise.SupervisordProgramPolicy(conf, "jevonsd")
	if err != nil {
		t.Fatal(err)
	}
	if p.Autorestart != "unexpected" || p.Relaunches() {
		t.Fatalf("policy = %+v", p)
	}
}

// The tracked template, rendered, is the healthy case.
func TestT1086TrackedTemplateRelaunches(t *testing.T) {
	tmpl, err := os.ReadFile(filepath.Join(supervisorDir(t), "jevonsd.ini"))
	if err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(t.TempDir(), "jevonsd.ini")
	t1086Write(t, conf, strings.ReplaceAll(string(tmpl), "@REPO@", "/repo"))
	p, err := supervise.SupervisordProgramPolicy(conf, "jevonsd")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Relaunches() || p.Autostart != "true" {
		t.Fatalf("tracked template policy = %+v", p)
	}
}

func TestT1086PolicyErrorsAreNamed(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "supervisord.conf")
	t1086Write(t, conf, "[include]\nfiles = d/*.ini\n")
	if _, err := supervise.SupervisordProgramPolicy(conf, "jevonsd"); err == nil || !strings.Contains(err.Error(), "no [program:jevonsd]") {
		t.Fatalf("missing program: err = %v", err)
	}
	t1086Write(t, filepath.Join(dir, "d", "a.ini"), "[program:jevonsd]\nautorestart=true\n")
	t1086Write(t, filepath.Join(dir, "d", "b.ini"), "[program:jevonsd]\nautorestart=false\n")
	if _, err := supervise.SupervisordProgramPolicy(conf, "jevonsd"); err == nil || !strings.Contains(err.Error(), "defined in both") {
		t.Fatalf("duplicate program: err = %v", err)
	}
	bad := filepath.Join(dir, "bad.conf")
	t1086Write(t, bad, "[program:jevonsd]\nautorestart=sometimes\n")
	if _, err := supervise.SupervisordProgramPolicy(bad, "jevonsd"); err == nil || !strings.Contains(err.Error(), "sometimes") {
		t.Fatalf("bad value: err = %v", err)
	}
	if _, err := supervise.SupervisordProgramPolicy(filepath.Join(dir, "absent.conf"), "jevonsd"); err == nil {
		t.Fatal("absent config: want error")
	}
}

func TestT1086ConfFromSupervisordArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{strings.Fields("/opt/homebrew/Cellar/python@3.13/x/Python /opt/homebrew/opt/supervisor/bin/supervisord -c /opt/homebrew/etc/supervisord.conf --nodaemon"), "/opt/homebrew/etc/supervisord.conf"},
		{[]string{"supervisord", "--configuration=/etc/s.conf"}, "/etc/s.conf"},
		{[]string{"supervisord", "--configuration", "/etc/s.conf"}, "/etc/s.conf"},
		{[]string{"supervisord", "-c/etc/s.conf"}, "/etc/s.conf"},
		{[]string{"supervisord", "-n"}, ""},
	} {
		if got := supervise.SupervisordConfFromArgs(tc.args); got != tc.want {
			t.Errorf("SupervisordConfFromArgs(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
